package stage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/cloudflarerecords"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/defaults"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/managedmarker"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
	"github.com/GoCodeAlone/workflow/config"
)

const CloudflareReportSchemaV1 = "workflow.dns-stage.cloudflare.report.v1"

type CloudflareOptions struct {
	PortfolioGlobs []string
	Scope          string
	DomainFilter   string
	StateDir       string
}

type CloudflareBundle struct {
	Config config.WorkflowConfig `json:"config" yaml:"config"`
	Report CloudflareReport      `json:"report" yaml:"report"`
}

type CloudflareReport struct {
	Schema              string                   `json:"schema" yaml:"schema"`
	Scope               string                   `json:"scope" yaml:"scope"`
	TotalCatalogDomains int                      `json:"total_catalog_domains" yaml:"total_catalog_domains"`
	SelectedDomains     int                      `json:"selected_domains" yaml:"selected_domains"`
	BlockedByScope      int                      `json:"blocked_by_scope" yaml:"blocked_by_scope"`
	Domains             []CloudflareDomainReport `json:"domains" yaml:"domains"`
}

type CloudflareDomainReport struct {
	Domain                   string         `json:"domain" yaml:"domain"`
	ResourceName             string         `json:"resource_name" yaml:"resource_name"`
	SelectedProvider         string         `json:"selected_provider" yaml:"selected_provider"`
	Classification           string         `json:"classification" yaml:"classification"`
	SafeForUnattendedCutover bool           `json:"safe_for_unattended_cutover" yaml:"safe_for_unattended_cutover"`
	Authority                map[string]any `json:"authority,omitempty" yaml:"authority,omitempty"`
	RecordCount              int            `json:"record_count" yaml:"record_count"`
}

type stagedDomain struct {
	report  CloudflareDomainReport
	records []map[string]any
}

func CompileCloudflare(opts CloudflareOptions) (*CloudflareBundle, error) {
	if len(opts.PortfolioGlobs) == 0 {
		return nil, fmt.Errorf("at least one portfolio path or glob is required")
	}
	if opts.Scope == "" {
		opts.Scope = "safe"
	}
	if opts.Scope != "safe" && opts.Scope != "all" {
		return nil, fmt.Errorf("unsupported cloudflare stage scope %q (want safe or all)", opts.Scope)
	}
	if opts.StateDir == "" {
		opts.StateDir = defaults.CloudflareStagingStateDir
	}
	snapshots, err := loadSnapshots(opts.PortfolioGlobs)
	if err != nil {
		return nil, err
	}
	domainFilter := normalizeDomain(opts.DomainFilter)
	grouped := groupSnapshots(snapshots, domainFilter)
	if len(grouped) == 0 {
		if domainFilter != "" {
			return nil, fmt.Errorf("domain %s not found in portfolios", domainFilter)
		}
		return nil, fmt.Errorf("no DNS snapshots found in portfolios")
	}

	domains := make([]string, 0, len(grouped))
	for domain := range grouped {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	all := make([]stagedDomain, 0, len(domains))
	selected := make([]stagedDomain, 0, len(domains))
	for _, domain := range domains {
		item := buildStagedDomain(domain, grouped[domain], opts.StateDir)
		all = append(all, item)
		if opts.Scope == "all" || item.report.SafeForUnattendedCutover {
			selected = append(selected, item)
		}
	}

	modules := []config.ModuleConfig{
		{
			Name: "cloudflare",
			Type: "iac.provider",
			Config: map[string]any{ //nolint:gosec // Values are env placeholders, not literal credentials.
				"provider":   "cloudflare",
				"api_token":  "${CLOUDFLARE_API_TOKEN}",
				"account_id": "${CLOUDFLARE_ACCOUNT_ID}",
			},
		},
		{
			Name: "iac-state",
			Type: "iac.state",
			Config: map[string]any{
				"backend":   "filesystem",
				"directory": opts.StateDir,
			},
		},
	}
	for _, item := range selected {
		modules = append(modules, config.ModuleConfig{
			Name: item.report.ResourceName,
			Type: "infra.dns",
			Config: map[string]any{
				"provider":        "cloudflare",
				"account_id":      "${CLOUDFLARE_ACCOUNT_ID}",
				"domain":          item.report.Domain,
				"manage_unlisted": false,
				"records":         item.records,
			},
		})
	}

	reports := make([]CloudflareDomainReport, 0, len(all))
	for _, item := range all {
		reports = append(reports, item.report)
	}
	return &CloudflareBundle{
		Config: config.WorkflowConfig{Modules: modules},
		Report: CloudflareReport{
			Schema:              CloudflareReportSchemaV1,
			Scope:               opts.Scope,
			TotalCatalogDomains: len(all),
			SelectedDomains:     len(selected),
			BlockedByScope:      len(all) - len(selected),
			Domains:             reports,
		},
	}, nil
}

func loadSnapshots(globs []string) ([]record.Snapshot, error) {
	var files []string
	for _, pattern := range globs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("portfolio glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			if _, statErr := os.Stat(pattern); statErr == nil {
				matches = []string{pattern}
			}
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no portfolio files matched")
	}
	sort.Strings(files)
	var snapshots []record.Snapshot
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read portfolio %q: %w", file, err)
		}
		var p record.Portfolio
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("parse portfolio %q: %w", file, err)
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("portfolio %q: %w", file, err)
		}
		snapshots = append(snapshots, p.Snapshots...)
	}
	return snapshots, nil
}

func groupSnapshots(snapshots []record.Snapshot, domainFilter string) map[string][]record.Snapshot {
	out := map[string][]record.Snapshot{}
	for _, snapshot := range snapshots {
		domain := normalizeDomain(snapshot.Domain)
		if domain == "" || (domainFilter != "" && domain != domainFilter) {
			continue
		}
		out[domain] = append(out[domain], snapshot)
	}
	return out
}

func buildStagedDomain(domain string, group []record.Snapshot, stateDir string) stagedDomain {
	sort.SliceStable(group, func(i, j int) bool {
		return sourceRank(group[i], group) < sourceRank(group[j], group)
	})
	source := group[0]
	authoritySnapshot := firstAuthoritySnapshot(group)
	currentAuthoritySource := currentAuthoritativeSource(group)
	classification := classify(group, source, authoritySnapshot)
	resource := resourceName("cf", domain)
	recordSource := source.Records
	if strings.EqualFold(source.Provider, "cloudflare") && currentAuthoritySource.Provider != "" && !strings.EqualFold(currentAuthoritySource.Provider, "cloudflare") {
		recordSource = append(append([]record.Record(nil), source.Records...), currentAuthoritySource.Records...)
	}
	recordSource = cloudflarerecords.EffectiveRecordSource(domain, group, source, recordSource, func(snapshot record.Snapshot) int {
		return sourceRank(snapshot, group)
	})
	records := managedmarker.Append(cloudflareRecords(domain, recordSource), stateDir, resource)
	report := CloudflareDomainReport{
		Domain:                   domain,
		ResourceName:             resource,
		SelectedProvider:         strings.ToLower(source.Provider),
		Classification:           classification,
		SafeForUnattendedCutover: safeClassification(classification),
		Authority:                authoritySnapshot.Authority,
		RecordCount:              len(records),
	}
	return stagedDomain{report: report, records: records}
}

func firstAuthoritySnapshot(group []record.Snapshot) record.Snapshot {
	for _, snapshot := range group {
		if len(snapshot.Authority) > 0 {
			return snapshot
		}
	}
	return record.Snapshot{}
}

func currentAuthoritativeSource(group []record.Snapshot) record.Snapshot {
	currentProvider := currentDelegationProvider(group)
	if currentProvider != "" {
		for _, snapshot := range group {
			if strings.EqualFold(snapshot.Provider, currentProvider) {
				return snapshot
			}
		}
	}
	for _, snapshot := range group {
		if nameserverProvider(snapshot) == strings.ToLower(snapshot.Provider) {
			return snapshot
		}
	}
	return record.Snapshot{}
}

func currentDelegationProvider(group []record.Snapshot) string {
	providers := map[string]bool{}
	for _, snapshot := range group {
		if provider := nameserverProvider(snapshot); provider != "" {
			providers[provider] = true
		}
	}
	if len(providers) != 1 {
		return ""
	}
	for provider := range providers {
		return provider
	}
	return ""
}

func classify(group []record.Snapshot, source, authoritySnapshot record.Snapshot) string {
	authorityProvider := nameserverProvider(authoritySnapshot)
	sourceProvider := strings.ToLower(source.Provider)
	switch {
	case sourceProvider == "cloudflare":
		return "existing_cloudflare"
	case authorityProvider != "" && sourceProvider == authorityProvider:
		return "authoritative_source_captured"
	case hasExternalAuthority(authoritySnapshot):
		return "external_authority_needs_manual_record_audit"
	case sourceProvider == "namecheap":
		return "provider_import_no_delegation_snapshot"
	default:
		return "staged_from_best_available_snapshot"
	}
}

func safeClassification(classification string) bool {
	switch classification {
	case "existing_cloudflare", "authoritative_source_captured", "provider_import_no_delegation_snapshot":
		return true
	default:
		return false
	}
}

func sourceRank(snapshot record.Snapshot, group []record.Snapshot) int {
	provider := strings.ToLower(snapshot.Provider)
	authorityProvider := nameserverProvider(snapshot)
	switch {
	case provider == "cloudflare":
		return 0
	case authorityProvider != "" && provider == authorityProvider:
		return 1
	case provider == "digitalocean" && hoverDelegatesTo(group, "digitalocean"):
		return 1
	case provider == "namecheap":
		return 2
	case provider == "hover" && authorityProvider == "hover":
		return 2
	case provider == "digitalocean":
		return 3
	case provider == "hover":
		return 4
	default:
		return 9
	}
}

func hoverDelegatesTo(group []record.Snapshot, provider string) bool {
	for _, snapshot := range group {
		if strings.EqualFold(snapshot.Provider, "hover") && nameserverProvider(snapshot) == provider {
			return true
		}
	}
	return false
}

func hasExternalAuthority(snapshot record.Snapshot) bool {
	return len(nameserverValues(snapshot)) > 0 && nameserverProvider(snapshot) == ""
}

func nameserverProvider(snapshot record.Snapshot) string {
	for _, value := range nameserverValues(snapshot) {
		switch {
		case strings.Contains(value, "cloudflare.com"):
			return "cloudflare"
		case strings.Contains(value, "digitalocean.com"):
			return "digitalocean"
		case strings.Contains(value, "hover.com"):
			return "hover"
		case strings.Contains(value, "registrar-servers.com"):
			return "namecheap"
		}
	}
	return ""
}

func nameserverValues(snapshot record.Snapshot) []string {
	values := append(stringSliceFromAuthority(snapshot.Authority, "live_nameservers"), stringSliceFromAuthority(snapshot.Authority, "registrar_nameservers")...)
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeDomain(value)
		if normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func stringSliceFromAuthority(authority map[string]any, key string) []string {
	if authority == nil {
		return nil
	}
	raw, ok := authority[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func cloudflareRecords(domain string, records []record.Record) []map[string]any {
	items := make([]map[string]any, 0, len(records))
	seen := map[string]bool{}
	dnsOnlyNames := cloudflareDNSOnlyNames(domain, records)
	for _, rec := range records {
		recType := strings.ToUpper(rec.Type)
		if !supportedCloudflareType(recType) {
			continue
		}
		name := cloudflarerecords.CloudflareName(domain, rec.Name)
		if recType == "NS" && (name == "@" || normalizeDomain(name) == domain) {
			continue
		}
		data, priority := recordDataAndPriority(rec)
		if recType == "CNAME" || recType == "MX" || recType == "NS" || recType == "SRV" {
			data = strings.TrimSuffix(data, ".")
		}
		if recType == "TXT" {
			data = quoteTXTData(data)
		}
		ttl := rec.TTL
		if ttl == 1 {
			ttl = 1
		} else if ttl < 60 {
			ttl = 60
		}
		item := map[string]any{
			"type": recType,
			"name": name,
			"data": data,
			"ttl":  ttl,
		}
		if shouldProxyCloudflareRecord(recType, name, dnsOnlyNames) {
			item["proxied"] = true
		}
		if recType == "MX" || recType == "SRV" {
			item["priority"] = priority
		}
		key := recordKey(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return recordKey(items[i]) < recordKey(items[j])
	})
	return items
}

func cloudflareDNSOnlyNames(domain string, records []record.Record) map[string]bool {
	names := map[string]bool{
		"autoconfig":   true,
		"autodiscover": true,
		"email":        true,
		"imap":         true,
		"mail":         true,
		"pop":          true,
		"pop3":         true,
		"smtp":         true,
		"webmail":      true,
	}
	domain = normalizeDomain(domain)
	for _, rec := range records {
		if !strings.EqualFold(rec.Type, "MX") {
			continue
		}
		data, _ := recordDataAndPriority(rec)
		target := normalizeDomain(data)
		if domain != "" && target == domain {
			names["@"] = true
			continue
		}
		if domain == "" || !strings.HasSuffix(target, "."+domain) {
			continue
		}
		relative := strings.TrimSuffix(target, "."+domain)
		if relative != "" {
			names[relative] = true
		}
	}
	return names
}

func shouldProxyCloudflareRecord(recType, name string, dnsOnlyNames map[string]bool) bool {
	switch strings.ToUpper(recType) {
	case "A", "AAAA", "CNAME":
	default:
		return false
	}
	normalized := normalizeDomain(defaultName(name))
	if normalized == "" || dnsOnlyNames[normalized] || cloudflarerecords.ContainsUnderscoreLabel(normalized) {
		return false
	}
	return true
}

func recordDataAndPriority(rec record.Record) (string, int) {
	data := rec.Value
	priority := 0
	if rec.Priority != nil {
		priority = *rec.Priority
		return data, priority
	}
	if strings.EqualFold(rec.Type, "MX") {
		fields := strings.Fields(data)
		if len(fields) >= 2 {
			if parsed, err := strconv.Atoi(fields[0]); err == nil {
				return strings.Join(fields[1:], " "), parsed
			}
		}
	}
	return data, priority
}

func quoteTXTData(data string) string {
	trimmed := strings.TrimSpace(data)
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		return data
	}
	return strconv.Quote(data)
}

func recordKey(item map[string]any) string {
	return strings.Join([]string{
		strings.ToLower(fmt.Sprint(item["type"])),
		strings.ToLower(fmt.Sprint(item["name"])),
		strings.ToLower(fmt.Sprint(item["data"])),
		fmt.Sprint(item["priority"]),
	}, "\x00")
}

func supportedCloudflareType(recType string) bool {
	switch recType {
	case "A", "AAAA", "CAA", "CNAME", "MX", "NS", "SRV", "TXT":
		return true
	default:
		return false
	}
}

func defaultName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "@"
	}
	return name
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

var resourceNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

func resourceName(prefix, domain string) string {
	slug := resourceNamePattern.ReplaceAllString(normalizeDomain(domain), "-")
	slug = strings.Trim(slug, "-")
	return prefix + "-" + slug
}
