package dnscli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/intent"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/policy"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
	"gopkg.in/yaml.v3"
)

const policyRecordPrefix = "_workflow-dns-policy."

type policyShowOptions struct {
	portfolioCSV string
	configFile   string
	provider     string
	zone         string
	raw          bool
}

type policyCheckOptions struct {
	portfolioCSV string
	configFile   string
	owner        string
}

func (c *CLI) runDNSPolicyShow(args []string) error {
	opts, err := parsePolicyShowOptions(args)
	if err != nil {
		return err
	}
	snapshots, err := loadPortfolioSnapshots(splitCSV(opts.portfolioCSV))
	if err != nil {
		return err
	}
	rrs := policyRRsForZone(snapshots, opts.zone, opts.provider)
	policyName := policyName(opts.zone)
	if len(rrs) == 0 {
		fmt.Printf("No policy found at %s\n", policyName)
		return nil
	}
	if opts.raw {
		for _, rr := range rrs {
			fmt.Println(rr)
		}
		return nil
	}
	pol, err := policy.Parse(opts.zone, rrs)
	if err != nil {
		return fmt.Errorf("dns policy show: parse: %w", err)
	}
	printPolicy(opts.zone, policyName, pol)
	return nil
}

func (c *CLI) runDNSPolicyCheck(args []string) error {
	opts, err := parsePolicyCheckOptions(args)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(opts.configFile)
	if err != nil {
		return fmt.Errorf("read generated config %q: %w", opts.configFile, err)
	}
	var cfg struct {
		Modules []struct {
			Name   string         `yaml:"name"`
			Type   string         `yaml:"type"`
			Config map[string]any `yaml:"config"`
		} `yaml:"modules"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse generated config %q: %w", opts.configFile, err)
	}
	snapshots, err := loadPortfolioSnapshots(splitCSV(opts.portfolioCSV))
	if err != nil {
		return err
	}
	for _, module := range cfg.Modules {
		if module.Type != "infra.dns" {
			continue
		}
		zone, _ := module.Config["domain"].(string)
		if strings.TrimSpace(zone) == "" {
			return fmt.Errorf("dns policy check: infra.dns module %q missing config.domain", module.Name)
		}
		pol, err := parsedPolicyForZone(snapshots, zone, providerNameFromConfig(module.Config))
		if err != nil {
			return err
		}
		for _, rec := range mapRecords(module.Config["records"]) {
			name, _ := rec["name"].(string)
			recordType, _ := rec["type"].(string)
			if name == "" || recordType == "" {
				continue
			}
			if err := pol.CheckAllowed(name, recordType, opts.owner); err != nil {
				return fmt.Errorf("dns policy check: zone=%s record=%s/%s owner=%s: %w", zone, name, recordType, opts.owner, err)
			}
		}
	}
	fmt.Fprintln(os.Stderr, "DNS policy check passed.")
	return nil
}

func enforceIntentDNSPolicy(bundle *intent.Bundle, opts compileOptions, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		fmt.Fprintln(os.Stderr, "warning: WORKFLOW_DNS_OWNER not set; skipping DNS policy check for domain intent apply")
		return nil
	}
	tmp, err := os.CreateTemp("", "wfctl-domain-intent-*.yaml")
	if err != nil {
		return fmt.Errorf("dns policy check: create temp generated config: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dns policy check: close temp generated config: %w", err)
	}
	data, err := yaml.Marshal(bundle.Config)
	if err != nil {
		return fmt.Errorf("dns policy check: marshal generated config: %w", err)
	}
	if err := os.WriteFile(tmp.Name(), data, 0o600); err != nil {
		return fmt.Errorf("dns policy check: write temp generated config: %w", err)
	}
	checkOpts := []string{
		"--portfolio", opts.portfolioCSV,
		"--config", tmp.Name(),
		"--owner", owner,
	}
	cli := &CLI{}
	return cli.runDNSPolicyCheck(checkOpts)
}

func parsePolicyShowOptions(args []string) (policyShowOptions, error) {
	fs := flag.NewFlagSet("dns policy show", flag.ContinueOnError)
	var opts policyShowOptions
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.configFile, "config", "", "Accepted for compatibility with wfctl dns-policy; ignored when --portfolio is used")
	fs.StringVar(&opts.configFile, "c", "", "Config file compatibility alias")
	fs.StringVar(&opts.provider, "provider", "", "Optional provider filter")
	fs.StringVar(&opts.zone, "zone", "", "DNS zone")
	fs.BoolVar(&opts.raw, "raw", false, "Print raw TXT RR values")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.zone == "" {
		return opts, fmt.Errorf("dns policy show requires --zone")
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("dns policy show: unexpected positional argument(s): %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parsePolicyCheckOptions(args []string) (policyCheckOptions, error) {
	fs := flag.NewFlagSet("dns policy check", flag.ContinueOnError)
	var opts policyCheckOptions
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.configFile, "config", "", "Generated wfctl config to check")
	fs.StringVar(&opts.configFile, "c", "", "Generated wfctl config to check")
	fs.StringVar(&opts.owner, "owner", "", "DNS owner identity")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.configFile == "" {
		return opts, fmt.Errorf("dns policy check requires --config")
	}
	if opts.owner == "" {
		return opts, fmt.Errorf("dns policy check requires --owner")
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("dns policy check: unexpected positional argument(s): %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func loadPortfolioSnapshots(globs []string) ([]record.Snapshot, error) {
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

func parsedPolicyForZone(snapshots []record.Snapshot, zone, provider string) (*policy.Policy, error) {
	rrs := policyRRsForZone(snapshots, zone, provider)
	if len(rrs) == 0 {
		return nil, fmt.Errorf("dns policy check: fail-closed: no policy found at %s", policyName(zone))
	}
	pol, err := policy.Parse(zone, rrs)
	if err != nil {
		return nil, fmt.Errorf("dns policy check: parse policy for %s: %w", zone, err)
	}
	if len(pol.Entries) == 0 {
		return nil, fmt.Errorf("dns policy check: fail-closed: no workflow policy entries found at %s", policyName(zone))
	}
	return pol, nil
}

func policyRRsForZone(snapshots []record.Snapshot, zone, provider string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	var fallback []string
	for _, snapshot := range snapshots {
		if !sameDNSName(snapshot.Domain, zone) {
			continue
		}
		rrs := policyRRsFromRecords(snapshot.Records, zone)
		if len(rrs) == 0 {
			continue
		}
		if provider == "" || strings.EqualFold(snapshot.Provider, provider) {
			return rrs
		}
		if fallback == nil {
			fallback = rrs
		}
	}
	return fallback
}

func policyRRsFromRecords(records []record.Record, zone string) []string {
	wantNames := map[string]bool{
		policyName(zone):       true,
		"_workflow-dns-policy": true,
	}
	var out []string
	for _, rec := range records {
		if !strings.EqualFold(rec.Type, "TXT") {
			continue
		}
		if wantNames[normalizeRecordName(rec.Name)] {
			out = append(out, strings.Trim(strings.TrimSpace(rec.Value), `"`))
		}
	}
	return out
}

func printPolicy(zone, policyName string, pol *policy.Policy) {
	fmt.Printf("DNS Ownership Policy for zone: %s\n", zone)
	fmt.Printf("TXT record: %s (%d RR(s))\n", policyName, len(pol.Entries))
	fmt.Println(strings.Repeat("-", 60))
	for _, e := range pol.Entries {
		marker := ""
		if e.Default {
			marker = " [DEFAULT]"
		}
		fmt.Printf("Owner: %s%s\n", e.Owner, marker)
		if len(e.Patterns) > 0 {
			fmt.Printf("  Patterns: %s\n", strings.Join(e.Patterns, ", "))
		} else {
			fmt.Printf("  Patterns: (catch-all default)\n")
		}
		if len(e.Types) > 0 {
			fmt.Printf("  Types:    %s\n", strings.Join(e.Types, ", "))
		} else {
			fmt.Printf("  Types:    all (except SOA/NS)\n")
		}
		fmt.Println()
	}
}

func policyName(zone string) string {
	return policyRecordPrefix + strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
}

func sameDNSName(a, b string) bool {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(a)), ".") == strings.TrimSuffix(strings.ToLower(strings.TrimSpace(b)), ".")
}

func normalizeRecordName(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

func providerNameFromConfig(config map[string]any) string {
	provider, _ := config["provider"].(string)
	return provider
}

func mapRecords(records any) []map[string]any {
	switch v := records.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), v...)
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if rec, ok := raw.(map[string]any); ok {
				out = append(out, rec)
			}
		}
		return out
	default:
		return nil
	}
}
