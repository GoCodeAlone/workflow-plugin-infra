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
	dns := bundle.Config.Modules[len(bundle.Config.Modules)-1]
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", dns.Config["records"])
	}

	var markers []map[string]any
	var googleTXT bool
	for _, record := range records {
		if record["type"] == "TXT" && strings.HasPrefix(record["name"].(string), "_workflow-dns-managed") {
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
