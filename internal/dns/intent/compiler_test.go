package intent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
	"github.com/GoCodeAlone/workflow/config"
)

func TestCompileDiscardParkedProducesCloudflareAndHoverResources(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "Example.COM.": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "nameserver_cutover": true,
      "records_policy": "discard_parked",
      "expected_current_nameservers": ["ns3.hover.com.", "ns1.hover.com", "ns2.hover.com"]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-com",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["ZOE.NS.CLOUDFLARE.COM.", "adam.ns.cloudflare.com"]}
    },
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com", "ns3.hover.com"]},
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*", "value": "216.40.34.41", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "CNAME", "name": "mail", "value": "mail.hover.com.cust.hostedemail.com", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 0 {
		t.Fatalf("blocked domains = %d; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	if bundle.Report.ActionCount != 2 {
		t.Fatalf("action count = %d, want 2", bundle.Report.ActionCount)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing cloudflare DNS module: %+v", bundle.Config.Modules)
	}
	if got.Type != "infra.dns" {
		t.Fatalf("cloudflare module type = %q", got.Type)
	}
	if got.Config["manage_unlisted"] != true {
		t.Fatalf("manage_unlisted = %#v, want true", got.Config["manage_unlisted"])
	}
	records, ok := got.Config["records"].([]map[string]any)
	if !ok || len(records) != 1 || managedMarkerRecord(records) == nil {
		t.Fatalf("records = %#v, want only managed marker", got.Config["records"])
	}
	delegation := moduleByName(bundle.Config.Modules, "hover-delegation-example-com")
	if delegation == nil || delegation.Type != "infra.dns_delegation" {
		t.Fatalf("missing hover delegation module: %+v", bundle.Config.Modules)
	}
	nameservers, ok := delegation.Config["nameservers"].([]string)
	if !ok {
		t.Fatalf("nameservers type = %T", delegation.Config["nameservers"])
	}
	wantNS := []string{"adam.ns.cloudflare.com", "zoe.ns.cloudflare.com"}
	if !equalStrings(nameservers, wantNS) {
		t.Fatalf("nameservers = %v, want %v", nameservers, wantNS)
	}
}

func TestCompileForwardToProducesCloudflareRedirectResource(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.net": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "records_policy": "discard_parked",
      "forward_to": "http://example.com"
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-net",
      "provider": "cloudflare",
      "domain": "example.net",
      "authority": {"name_servers": ["ada.ns.cloudflare.com", "bob.ns.cloudflare.com"]},
      "records": []
    },
    {
      "id": "hover-example-net",
      "provider": "hover",
      "domain": "example.net",
      "authority": {"registrar_nameservers": ["ada.ns.cloudflare.com", "bob.ns.cloudflare.com"]},
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*", "value": "216.40.34.41", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "CNAME", "name": "mail", "value": "mail.hover.com.cust.hostedemail.com", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 0 {
		t.Fatalf("blocked domains = %d; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	if bundle.Report.ActionCount != 2 {
		t.Fatalf("action count = %d, want stage_dns + configure_redirect", bundle.Report.ActionCount)
	}
	dns := moduleByName(bundle.Config.Modules, "cf-example-net")
	if dns == nil {
		t.Fatalf("missing cloudflare DNS module: %+v", bundle.Config.Modules)
	}
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok || len(records) != 2 || managedMarkerRecord(records) == nil {
		t.Fatalf("dns records = %#v, want originless placeholder plus managed marker", dns.Config["records"])
	}
	if records[0]["type"] != "A" || records[0]["name"] != "@" || records[0]["data"] != "192.0.2.1" || records[0]["proxied"] != true {
		t.Fatalf("placeholder record = %#v", records[0])
	}
	redirect := moduleByName(bundle.Config.Modules, "cf-redirect-example-net")
	if redirect == nil {
		t.Fatalf("missing redirect module: %+v", bundle.Config.Modules)
	}
	if redirect.Type != "infra.http_redirect" {
		t.Fatalf("redirect type = %q", redirect.Type)
	}
	if redirect.Config["provider"] != "cloudflare" {
		t.Fatalf("provider = %#v", redirect.Config["provider"])
	}
	if redirect.Config["domain"] != "example.net" || redirect.Config["from_host"] != "example.net" {
		t.Fatalf("redirect config = %#v", redirect.Config)
	}
	if redirect.Config["target_url"] != "http://example.com" {
		t.Fatalf("target_url = %#v", redirect.Config["target_url"])
	}
	if redirect.Config["status_code"] != 301 {
		t.Fatalf("status_code = %#v", redirect.Config["status_code"])
	}
}

func TestCompileAddsManagedByTXTMarker(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "records_policy": "preserve_cloudflare"
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
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
}`)

	bundle, err := Compile(Options{
		IntentPath:     intentPath,
		PortfolioGlobs: []string{portfolioPath},
		StateDir:       ".state/domain-intent-test/",
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	dns := moduleByName(bundle.Config.Modules, "cf-example-com")
	if dns == nil {
		t.Fatalf("missing cloudflare DNS module: %+v", bundle.Config.Modules)
	}
	records, ok := dns.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", dns.Config["records"])
	}
	marker := managedMarkerRecord(records)
	if marker == nil {
		t.Fatalf("records missing managed marker: %#v", records)
	}
	if marker["type"] != "TXT" || marker["name"] != "_workflow-dns-managed" || marker["ttl"] != 300 {
		t.Fatalf("marker = %#v, want TXT _workflow-dns-managed ttl 300", marker)
	}
	data, _ := marker["data"].(string)
	for _, want := range []string{"heritage=wfinfra-v1", "managed_by=wfctl", "state_dir=.state/cloudflare-staging/", "resource=cf-example-com"} {
		if !strings.Contains(data, want) {
			t.Fatalf("marker data = %q, missing %q", data, want)
		}
	}
	if strings.Contains(data, ".state/domain-intent-test/") {
		t.Fatalf("marker data should not use domain reconcile state dir: %q", data)
	}
}

func TestCompileDiscardParkedBlocksNonParkedRecords(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "records_policy": "discard_parked"
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {"id": "cf", "provider": "cloudflare", "domain": "example.com", "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]}},
    {"id": "hover", "provider": "hover", "domain": "example.com", "records": [{"type": "MX", "name": "@", "value": "mail.protonmail.ch", "ttl": 900}]}
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 1 {
		t.Fatalf("blocked domains = %d, want 1", bundle.Report.BlockedDomains)
	}
	if bundle.Report.ActionCount != 0 {
		t.Fatalf("action count = %d, want 0", bundle.Report.ActionCount)
	}
	if len(bundle.Report.Domains[0].Blockers) == 0 {
		t.Fatalf("expected blockers: %+v", bundle.Report.Domains[0])
	}
}

func TestCompileRejectsDuplicateNormalizedDomains(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "Example.COM": {"registrar": "hover", "dns_host": "cloudflare"},
    "example.com.": {"registrar": "hover", "dns_host": "cloudflare"}
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [{"id": "cf", "provider": "cloudflare", "domain": "example.com", "authority": {"name_servers": ["a.ns.cloudflare.com"]}}]
}`)

	_, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err == nil || !strings.Contains(err.Error(), "duplicate domain intent") {
		t.Fatalf("expected duplicate normalized domain error; got %v", err)
	}
}

func TestCompileBlocksExpectedCurrentNameserverMismatch(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "nameserver_cutover": true,
      "expected_current_nameservers": ["ns1.hover.com", "ns2.hover.com"]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {"id": "cf", "provider": "cloudflare", "domain": "example.com", "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]}},
    {"id": "hover", "provider": "hover", "domain": "example.com", "authority": {"registrar_nameservers": ["ns1.digitalocean.com", "ns2.digitalocean.com"]}}
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 1 {
		t.Fatalf("blocked domains = %d, want 1", bundle.Report.BlockedDomains)
	}
	got := strings.Join(bundle.Report.Domains[0].Blockers, "\n")
	if !strings.Contains(got, "did not match expected") {
		t.Fatalf("expected nameserver mismatch blocker; got %q", got)
	}
	if len(bundle.Config.Modules) != 1 {
		t.Fatalf("blocked compile should only emit state module; got %+v", bundle.Config.Modules)
	}
}

func TestCompilePreserveAuthoritativeIgnoresStagedCloudflareByDefault(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": []
    },
    {
      "id": "hover",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]},
      "records": [{"type": "A", "name": "@", "value": "192.0.2.44", "ttl": 900}]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing generated Cloudflare module: %+v", bundle.Config.Modules)
	}
	records, ok := got.Config["records"].([]map[string]any)
	if !ok || len(records) != 2 || managedMarkerRecord(records) == nil {
		t.Fatalf("records = %#v, want one authoritative Hover record plus managed marker", got.Config["records"])
	}
	if records[0]["data"] != "192.0.2.44" {
		t.Fatalf("record data = %#v, want authoritative Hover value", records[0]["data"])
	}
}

func TestCompileWebTargetReplacesAuthoritativeWebRecords(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "web_target": "gocodealone-multisite-zeqkn.ondigitalocean.app.",
      "web_hosts": ["@", "www", "*"]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": []
    },
    {
      "id": "hover",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns12.wixdns.net", "ns13.wixdns.net"]},
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*", "value": "216.40.34.41", "ttl": 900},
        {"type": "CNAME", "name": "www", "value": "example.wixdns.net.", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com.", "ttl": 900},
        {"type": "CNAME", "name": "mail", "value": "mail.hover.com.cust.hostedemail.com.", "ttl": 900},
        {"type": "TXT", "name": "@", "value": "google-site-verification=abc123", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 0 {
		t.Fatalf("blocked domains = %d; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing generated Cloudflare module: %+v", bundle.Config.Modules)
	}
	records, ok := got.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", got.Config["records"])
	}
	if got.Config["manage_unlisted"] != true {
		t.Fatalf("manage_unlisted = %#v, want true for web target cutover", got.Config["manage_unlisted"])
	}
	target := "gocodealone-multisite-zeqkn.ondigitalocean.app"
	for _, host := range []string{"@", "www", "*"} {
		rec := recordByTypeName(records, "CNAME", host)
		if rec == nil {
			t.Fatalf("missing web target record %s in %#v", host, records)
		}
		if rec["data"] != target || rec["ttl"] != 1 || rec["proxied"] != true {
			t.Fatalf("web target record %s = %#v", host, rec)
		}
	}
	if stale := recordByTypeName(records, "A", "@"); stale != nil {
		t.Fatalf("stale apex A record retained: %#v", stale)
	}
	if mx := recordByTypeName(records, "MX", "@"); mx == nil || mx["data"] != "mx.hover.com.cust.hostedemail.com" {
		t.Fatalf("MX record = %#v, want preserved mail routing", mx)
	}
	if mail := recordByTypeName(records, "CNAME", "mail"); mail == nil || mail["data"] != "mail.hover.com.cust.hostedemail.com" {
		t.Fatalf("mail CNAME = %#v, want preserved mail alias", mail)
	}
	if txt := recordByTypeName(records, "TXT", "@"); txt == nil || txt["data"] != `"google-site-verification=abc123"` {
		t.Fatalf("TXT record = %#v, want preserved quoted TXT", txt)
	}
}

func TestCompileDiscardParkedWebTargetPreservesNonWebRecords(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "records_policy": "discard_parked",
      "web_target": "gocodealone-multisite-zeqkn.ondigitalocean.app",
      "web_hosts": ["@", "*"]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": []
    },
    {
      "id": "hover",
      "provider": "hover",
      "domain": "example.com",
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "A", "name": "*", "value": "216.40.34.41", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "CNAME", "name": "mail", "value": "mail.hover.com.cust.hostedemail.com", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 0 {
		t.Fatalf("blocked domains = %d; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing generated Cloudflare module: %+v", bundle.Config.Modules)
	}
	if got.Config["manage_unlisted"] != true {
		t.Fatalf("manage_unlisted = %#v, want true", got.Config["manage_unlisted"])
	}
	records := got.Config["records"].([]map[string]any)
	if mx := recordByTypeName(records, "MX", "@"); mx == nil {
		t.Fatalf("MX record was not preserved: %#v", records)
	}
	if mail := recordByTypeName(records, "CNAME", "mail"); mail == nil {
		t.Fatalf("mail CNAME was not preserved: %#v", records)
	}
	for _, host := range []string{"@", "*"} {
		if web := recordByTypeName(records, "CNAME", host); web == nil || web["proxied"] != true {
			t.Fatalf("web target record %s = %#v", host, web)
		}
	}
}

func TestCompileWebTargetBlocksEmptyWebHosts(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "web_target": "gocodealone-multisite-zeqkn.ondigitalocean.app",
      "web_hosts": [" ", "."]
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {"id": "cf", "provider": "cloudflare", "domain": "example.com", "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]}},
    {"id": "hover", "provider": "hover", "domain": "example.com", "records": [{"type": "A", "name": "@", "value": "192.0.2.44", "ttl": 900}]}
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if bundle.Report.BlockedDomains != 1 {
		t.Fatalf("blocked domains = %d, want 1; report=%+v", bundle.Report.BlockedDomains, bundle.Report)
	}
	got := strings.Join(bundle.Report.Domains[0].Blockers, "\n")
	if !strings.Contains(got, "web_hosts") {
		t.Fatalf("blockers = %q, want web_hosts error", got)
	}
}

func TestCompilePreserveAuthoritativeNormalizesCloudflareRecords(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeTestFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true
    }
  }
}`)
	portfolioPath := writeTestFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": []
    },
    {
      "id": "hover",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]},
      "records": [
        {"type": "TXT", "name": "@", "value": "google-site-verification=abc123", "ttl": 900},
        {"type": "TXT", "name": "@", "value": "google-site-verification=abc123", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.example.com.", "ttl": 900}
      ]
    }
  ]
}`)

	bundle, err := Compile(Options{IntentPath: intentPath, PortfolioGlobs: []string{portfolioPath}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := moduleByName(bundle.Config.Modules, "cf-example-com")
	if got == nil {
		t.Fatalf("missing generated Cloudflare module: %+v", bundle.Config.Modules)
	}
	records, ok := got.Config["records"].([]map[string]any)
	if !ok {
		t.Fatalf("records = %T, want []map[string]any", got.Config["records"])
	}
	var txt, mx map[string]any
	for _, rec := range records {
		switch rec["type"] {
		case "TXT":
			if rec["name"] == "@" {
				if txt != nil {
					t.Fatalf("duplicate TXT record emitted: %#v", records)
				}
				txt = rec
			}
		case "MX":
			mx = rec
		}
	}
	if txt == nil || txt["data"] != `"google-site-verification=abc123"` {
		t.Fatalf("TXT record = %#v, want quoted data", txt)
	}
	if mx == nil || mx["data"] != "mx.example.com" || mx["priority"] != 10 {
		t.Fatalf("MX record = %#v, want parsed priority and trimmed target", mx)
	}
}

func TestCurrentAuthorityProviderIsOrderIndependent(t *testing.T) {
	group := []record.Snapshot{
		{
			Provider: "cloudflare",
			Domain:   "example.com",
			Authority: map[string]any{
				"name_servers": []any{"a.ns.cloudflare.com", "b.ns.cloudflare.com"},
			},
		},
		{
			Provider: "hover",
			Domain:   "example.com",
			Authority: map[string]any{
				"registrar_nameservers": []any{"ns1.hover.com", "ns2.hover.com"},
			},
		},
	}
	reversed := []record.Snapshot{group[1], group[0]}
	if got := currentAuthorityProvider(group); got != "hover" {
		t.Fatalf("currentAuthorityProvider(group) = %q, want hover", got)
	}
	if got := currentAuthorityProvider(reversed); got != "hover" {
		t.Fatalf("currentAuthorityProvider(reversed) = %q, want hover", got)
	}
}

func moduleByName(modules []config.ModuleConfig, name string) *config.ModuleConfig {
	for i := range modules {
		if modules[i].Name == name {
			return &modules[i]
		}
	}
	return nil
}

func managedMarkerRecord(records []map[string]any) map[string]any {
	for _, record := range records {
		if record["type"] == "TXT" && record["name"] == "_workflow-dns-managed" {
			return record
		}
	}
	return nil
}

func recordByTypeName(records []map[string]any, recType, name string) map[string]any {
	for _, record := range records {
		if record["type"] == recType && record["name"] == name {
			return record
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeTestFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
