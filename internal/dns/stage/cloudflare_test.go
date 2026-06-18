package stage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
	"github.com/GoCodeAlone/workflow/config"
)

func TestCompileCloudflareAddsManagedByTXTMarker(t *testing.T) {
	dir := t.TempDir()
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-com",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": [{"type": "A", "name": "@", "value": "192.0.2.10", "ttl": 300}]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}

	bundle, err := CompileCloudflare(CloudflareOptions{
		PortfolioGlobs: []string{portfolioPath},
		Scope:          "safe",
		StateDir:       ".state/cloudflare-staging-test/",
	})
	if err != nil {
		t.Fatalf("CompileCloudflare: %v", err)
	}
	if len(bundle.Config.Modules) < 1 {
		t.Fatalf("generated no modules")
	}
	dns := bundle.Config.Modules[len(bundle.Config.Modules)-1]
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", dns.Config["records"])
	}
	var marker map[string]any
	for _, record := range records {
		if record["type"] == "TXT" && record["name"] == "_workflow-dns-managed" {
			marker = record
			break
		}
	}
	if marker == nil {
		t.Fatalf("records missing managed marker: %#v", records)
	}
	data, _ := marker["data"].(string)
	for _, want := range []string{"heritage=wfinfra-v1", "managed_by=wfctl", "state_dir=.state/cloudflare-staging-test/", "resource=cf-example-com"} {
		if !strings.Contains(data, want) {
			t.Fatalf("marker data = %q, missing %q", data, want)
		}
	}
}

func TestCompileCloudflareReplacesImportedManagedMarkers(t *testing.T) {
	dir := t.TempDir()
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-gigbagg-rocks",
      "provider": "cloudflare",
      "domain": "gigbagg.rocks",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": [
        {"type": "TXT", "name": "_workflow-dns-managed.gigbagg.rocks", "value": "\"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/cloudflare-staging/ resource=cf-gigbagg-rocks\"", "ttl": 300},
        {"type": "TXT", "name": "_workflow-dns-managed.gigbagg.rocks", "value": "\"heritage=wfinfra-v1 managed_by=wfctl state_dir=.state/domain-reconcile/ resource=cf-gigbagg-rocks\"", "ttl": 300},
        {"type": "TXT", "name": "@", "value": "google-site-verification=abc123", "ttl": 300}
      ]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}

	bundle, err := CompileCloudflare(CloudflareOptions{
		PortfolioGlobs: []string{portfolioPath},
		Scope:          "safe",
		StateDir:       ".state/cloudflare-staging-test/",
	})
	if err != nil {
		t.Fatalf("CompileCloudflare: %v", err)
	}
	if len(bundle.Config.Modules) < 1 {
		t.Fatalf("generated no modules")
	}
	dns := bundle.Config.Modules[len(bundle.Config.Modules)-1]
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", dns.Config["records"])
	}

	var markers []map[string]any
	var googleTXT bool
	for _, record := range records {
		name, _ := record["name"].(string)
		normalizedName := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		if record["type"] == "TXT" && (normalizedName == "_workflow-dns-managed" || strings.HasPrefix(normalizedName, "_workflow-dns-managed.")) {
			markers = append(markers, record)
		}
		if record["type"] == "TXT" && record["name"] == "@" && record["data"] == `"google-site-verification=abc123"` {
			googleTXT = true
		}
	}
	if len(markers) != 1 {
		t.Fatalf("managed markers = %#v, want exactly one generated marker", markers)
	}
	data, _ := markers[0]["data"].(string)
	for _, want := range []string{"state_dir=.state/cloudflare-staging-test/", "resource=cf-gigbagg-rocks"} {
		if !strings.Contains(data, want) {
			t.Fatalf("marker data = %q, missing %q", data, want)
		}
	}
	if strings.Contains(data, ".state/domain-reconcile/") {
		t.Fatalf("marker data retained stale state dir: %q", data)
	}
	if !googleTXT {
		t.Fatalf("records missing non-marker TXT: %#v", records)
	}
}

func TestCompileCloudflareMergesCurrentAuthoritativeRecordsIntoExistingTargetZone(t *testing.T) {
	dir := t.TempDir()
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cloudflare-gocodealone-tech",
      "provider": "cloudflare",
      "domain": "gocodealone.tech",
      "authority": {
        "name_servers": ["amos.ns.cloudflare.com", "mckinley.ns.cloudflare.com"],
        "original_name_servers": ["ns1.digitalocean.com", "ns2.digitalocean.com", "ns3.digitalocean.com"],
        "role": "target_authoritative_dns"
      },
      "records": [
        {"type": "A", "name": "gocodealone.tech", "value": "162.159.140.98", "ttl": 60},
        {"type": "CNAME", "name": "admin.gocodealone.tech", "value": "gocodealone-multisite-zeqkn.ondigitalocean.app", "ttl": 1800},
        {"type": "CNAME", "name": "www.gocodealone.tech", "value": "gocodealone-multisite-zeqkn.ondigitalocean.app", "ttl": 1800}
      ]
    },
    {
      "id": "digitalocean-gocodealone-tech",
      "provider": "digitalocean",
      "domain": "gocodealone.tech",
      "authority": {
        "name_servers": ["ns1.digitalocean.com", "ns2.digitalocean.com", "ns3.digitalocean.com"],
        "role": "authoritative_dns"
      },
      "records": [
        {"type": "A", "name": "@", "value": "162.159.140.98", "ttl": 30},
        {"type": "CNAME", "name": "*.preview", "value": "gocodealone-multisite-zeqkn.ondigitalocean.app", "ttl": 1800},
        {"type": "CNAME", "name": "admin", "value": "gocodealone-multisite-zeqkn.ondigitalocean.app", "ttl": 1800},
        {"type": "CNAME", "name": "www", "value": "gocodealone-multisite-zeqkn.ondigitalocean.app", "ttl": 1800}
      ]
    },
    {
      "id": "hover-gocodealone-tech",
      "provider": "hover",
      "domain": "gocodealone.tech",
      "authority": {
        "live_nameservers": ["ns1.digitalocean.com", "ns2.digitalocean.com", "ns3.digitalocean.com"],
        "registrar_nameservers": ["ns1.digitalocean.com", "ns2.digitalocean.com", "ns3.digitalocean.com"]
      },
      "records": []
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}

	bundle, err := CompileCloudflare(CloudflareOptions{
		PortfolioGlobs: []string{portfolioPath},
		Scope:          "all",
		StateDir:       ".state/cloudflare-staging-test/",
	})
	if err != nil {
		t.Fatalf("CompileCloudflare: %v", err)
	}
	if len(bundle.Config.Modules) < 1 {
		t.Fatalf("generated no modules")
	}
	dns := bundle.Config.Modules[len(bundle.Config.Modules)-1]
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", dns.Config["records"])
	}
	counts := map[string]int{}
	for _, record := range records {
		name, _ := record["name"].(string)
		if strings.HasSuffix(name, ".gocodealone.tech") {
			t.Fatalf("record kept fully-qualified Cloudflare name %q: %#v", name, records)
		}
		counts[strings.Join([]string{
			strings.ToUpper(record["type"].(string)),
			name,
			record["data"].(string),
		}, "\x00")]++
	}
	for key, count := range counts {
		if count != 1 {
			t.Fatalf("record %q count = %d, want one: %#v", key, count, records)
		}
	}
	wantKey := strings.Join([]string{"CNAME", "*.preview", "gocodealone-multisite-zeqkn.ondigitalocean.app"}, "\x00")
	if counts[wantKey] != 1 {
		t.Fatalf("records missing authoritative *.preview CNAME: %#v", records)
	}
}

func TestCloudflareRecordsQuotesTXTWithoutTrimmingValue(t *testing.T) {
	records := cloudflareRecords("example.com", []record.Record{
		{Type: "TXT", Name: "@", Value: "  token with edge spaces  ", TTL: 300},
	})
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one TXT", records)
	}
	if got, want := records[0]["data"], `"  token with edge spaces  "`; got != want {
		t.Fatalf("TXT data = %#v, want %#v", got, want)
	}
}

func TestCloudflareRecordsDefaultsProxiableRecordsToProxied(t *testing.T) {
	records := cloudflareRecords("example.com", []record.Record{
		{Type: "A", Name: "@", Value: "192.0.2.10", TTL: 300},
		{Type: "AAAA", Name: "ipv6", Value: "2001:db8::10", TTL: 300},
		{Type: "CNAME", Name: "www", Value: "example.com.", TTL: 300},
		{Type: "CNAME", Name: "mail", Value: "mail.example.net.", TTL: 300},
		{Type: "A", Name: "mx", Value: "192.0.2.20", TTL: 300},
		{Type: "MX", Name: "@", Value: "10 mx.example.com.", TTL: 300},
		{Type: "TXT", Name: "@", Value: "v=spf1 -all", TTL: 300},
	})

	for _, tc := range []struct {
		recordType string
		name       string
		wantSet    bool
		wantValue  bool
	}{
		{"A", "@", true, true},
		{"AAAA", "ipv6", true, true},
		{"CNAME", "www", true, true},
		{"CNAME", "mail", false, false},
		{"A", "mx", false, false},
		{"MX", "@", false, false},
		{"TXT", "@", false, false},
	} {
		rec := stageRecordByTypeName(records, tc.recordType, tc.name)
		if rec == nil {
			t.Fatalf("missing %s %s in records: %#v", tc.recordType, tc.name, records)
		}
		gotValue, gotSet := rec["proxied"]
		if gotSet != tc.wantSet || (gotSet && gotValue != tc.wantValue) {
			t.Fatalf("%s %s proxied = (%#v, %v), want (%#v, %v); record=%#v", tc.recordType, tc.name, gotValue, gotSet, tc.wantValue, tc.wantSet, rec)
		}
	}
}

func TestCompileCloudflareReplacesParkedWebRecordsWithAlternateLiveSource(t *testing.T) {
	dir := t.TempDir()
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": [
        {"type": "A", "name": "example.com", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*.example.com", "value": "216.40.34.41", "ttl": 900},
        {"type": "CNAME", "name": "mail.example.com", "value": "mail.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "MX", "name": "example.com", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900}
      ]
    },
    {
      "id": "do",
      "provider": "digitalocean",
      "domain": "example.com",
      "authority": {"name_servers": ["ns1.digitalocean.com", "ns2.digitalocean.com", "ns3.digitalocean.com"]},
      "records": [
        {"type": "A", "name": "@", "value": "203.0.113.20", "ttl": 1800},
        {"type": "NS", "name": "@", "value": "ns1.digitalocean.com", "ttl": 1800},
        {"type": "SOA", "name": "@", "value": "ns1.digitalocean.com hostmaster.example.com 1 10800 3600 604800 1800", "ttl": 1800}
      ]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}

	bundle, err := CompileCloudflare(CloudflareOptions{
		PortfolioGlobs: []string{portfolioPath},
		Scope:          "all",
		StateDir:       ".state/cloudflare-staging-test/",
	})
	if err != nil {
		t.Fatalf("CompileCloudflare: %v", err)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing generated Cloudflare module: %+v", bundle.Config.Modules)
	}
	records := got.Config["records"].([]map[string]any)
	if parked := stageRecordByTypeData(records, "A", "216.40.34.41"); parked != nil {
		t.Fatalf("parked A record preserved: %#v in %#v", parked, records)
	}
	apex := stageRecordByTypeName(records, "A", "@")
	if apex == nil || apex["data"] != "203.0.113.20" || apex["proxied"] != true {
		t.Fatalf("apex A = %#v, want proxied DigitalOcean record", apex)
	}
	if mail := stageRecordByTypeName(records, "CNAME", "mail"); mail == nil || mail["data"] != "mail.hover.com.cust.hostedemail.com" {
		t.Fatalf("mail CNAME = %#v, want preserved Hover mail target", mail)
	}
	if mx := stageRecordByTypeName(records, "MX", "@"); mx == nil || mx["data"] != "mx.hover.com.cust.hostedemail.com" {
		t.Fatalf("MX = %#v, want preserved Hover MX", mx)
	}
}

func TestCloudflareRecordsKeepApexMXTargetDNSOnly(t *testing.T) {
	records := cloudflareRecords("example.com", []record.Record{
		{Type: "A", Name: "@", Value: "192.0.2.10", TTL: 300},
		{Type: "MX", Name: "@", Value: "10 example.com.", TTL: 300},
	})

	apex := stageRecordByTypeName(records, "A", "@")
	if apex == nil {
		t.Fatalf("missing apex A in records: %#v", records)
	}
	if proxied, ok := apex["proxied"]; ok {
		t.Fatalf("apex A proxied = %#v, want omitted because apex is an MX target; record=%#v", proxied, apex)
	}
}

func stageRecordByTypeName(records []map[string]any, recordType, name string) map[string]any {
	for _, rec := range records {
		if rec["type"] == recordType && rec["name"] == name {
			return rec
		}
	}
	return nil
}

func moduleByName(modules []config.ModuleConfig, name string) *config.ModuleConfig {
	for i := range modules {
		if modules[i].Name == name {
			return &modules[i]
		}
	}
	return nil
}

func stageRecordByTypeData(records []map[string]any, recordType, data string) map[string]any {
	for _, rec := range records {
		if rec["type"] == recordType && rec["data"] == data {
			return rec
		}
	}
	return nil
}
