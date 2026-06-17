package stage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
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
