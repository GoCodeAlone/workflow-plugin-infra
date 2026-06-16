package dnscli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunCLIHelpSucceeds(t *testing.T) {
	if code := New().RunCLI([]string{"dns", "--help"}); code != 0 {
		t.Fatalf("RunCLI help exit = %d, want 0", code)
	}
}

func TestRunDNSStageCloudflareWritesConfigAndReport(t *testing.T) {
	dir := t.TempDir()
	portfolioPath := writeFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]},
      "records": [
        {"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900},
        {"type": "MX", "name": "@", "value": "10 mx.hover.com.cust.hostedemail.com", "ttl": 900},
        {"type": "NS", "name": "@", "value": "ns1.hover.com", "ttl": 900}
      ]
    },
    {
      "id": "wix-example-net",
      "provider": "hover",
      "domain": "example.net",
      "authority": {"registrar_nameservers": ["ns12.wixdns.net", "ns13.wixdns.net"]},
      "records": [{"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900}]
    },
    {
      "id": "do-example-org",
      "provider": "digitalocean",
      "domain": "example.org",
      "records": [{"type": "A", "name": "@", "value": "192.0.2.50", "ttl": 300}]
    },
    {
      "id": "hover-example-org",
      "provider": "hover",
      "domain": "example.org",
      "records": [{"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900}]
    }
  ]
}`)
	outputPath := filepath.Join(dir, "cloudflare.yaml")
	reportPath := filepath.Join(dir, "report.json")

	code := New().RunCLI([]string{"dns", "stage", "cloudflare",
		"--portfolio", portfolioPath,
		"--scope", "safe",
		"--output", outputPath,
		"--report", reportPath,
		"--state-dir", ".state/test-cloudflare/",
	})
	if code != 0 {
		t.Fatalf("RunCLI stage cloudflare exit = %d, want 0", code)
	}

	cfgData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	var cfg struct {
		Modules []yamlTestModule `yaml:"modules"`
	}
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil {
		t.Fatalf("parse output config: %v\n%s", err, cfgData)
	}
	dns := yamlModuleByName(cfg.Modules, "cf-example-com")
	if dns == nil {
		t.Fatalf("generated config missing cf-example-com: %#v", cfg.Modules)
	}
	if dns.Type != "infra.dns" {
		t.Fatalf("module type = %q, want infra.dns", dns.Type)
	}
	records, ok := dns.Config["records"].([]any)
	if !ok || len(records) != 2 {
		t.Fatalf("records = %#v, want A+MX only", dns.Config["records"])
	}
	if blocked := yamlModuleByName(cfg.Modules, "cf-example-net"); blocked != nil {
		t.Fatalf("safe scope should exclude external authority module: %#v", blocked)
	}

	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report struct {
		Schema          string `json:"schema"`
		Scope           string `json:"scope"`
		SelectedDomains int    `json:"selected_domains"`
		BlockedByScope  int    `json:"blocked_by_scope"`
		Domains         []struct {
			Domain         string `json:"domain"`
			Classification string `json:"classification"`
			RecordCount    int    `json:"record_count"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, reportData)
	}
	if report.Schema != "workflow.dns-stage.cloudflare.report.v1" || report.Scope != "safe" || report.SelectedDomains != 1 || report.BlockedByScope != 2 {
		t.Fatalf("bad report summary: %+v", report)
	}
	if len(report.Domains) != 3 {
		t.Fatalf("report domains = %d, want 3", len(report.Domains))
	}
}

func TestRunDNSIntentReconcilePlansWithGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
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
	portfolioPath := writeFile(t, dir, "portfolio.json", `{
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
	outputPath := filepath.Join(dir, "out.yaml")
	reportPath := filepath.Join(dir, "report.json")
	planPath := filepath.Join(dir, "plan.json")
	var calls [][]string
	cli := &CLI{
		runCommand: func(name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
	}

	code := cli.RunCLI([]string{"dns", "intent", "reconcile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", outputPath,
		"--report", reportPath,
		"--plan-output", planPath,
		"--plugin-dir", filepath.Join(dir, "plugins"),
	})
	if code != 0 {
		t.Fatalf("RunCLI reconcile exit = %d, want 0", code)
	}
	want := [][]string{
		{"wfctl", "validate", "--allow-no-entry-points", "--plugin-dir", filepath.Join(dir, "plugins"), outputPath},
		{"wfctl", "infra", "plan", "--config", outputPath, "--plugin-dir", filepath.Join(dir, "plugins"), "--output", planPath},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", calls, want)
	}
}

func TestRunDNSIntentReconcileApplyRequiresAutoApprove(t *testing.T) {
	code := New().RunCLI([]string{"dns", "intent", "reconcile", "--mode", "apply"})
	if code == 0 {
		t.Fatal("apply without --auto-approve should fail")
	}
}

func TestRunDNSIntentReconcileRejectsUnexpectedArgs(t *testing.T) {
	code := New().RunCLI([]string{"dns", "intent", "reconcile", "extra"})
	if code == 0 {
		t.Fatal("reconcile with unexpected positional args should fail")
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestRunCLIIntentCompileRejectsUnexpectedArgs(t *testing.T) {
	code := New().RunCLI([]string{"dns", "intent", "compile", "extra"})
	if code == 0 {
		t.Fatal("compile with unexpected positional args should fail")
	}
}

func TestRunCLIIntentCompileWritesConfigAndReport(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "domains.json")
	if err := os.WriteFile(intentPath, []byte(`{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true,
      "nameserver_cutover": false,
      "records_policy": "preserve_cloudflare"
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	portfolioPath := filepath.Join(dir, "portfolio.json")
	if err := os.WriteFile(portfolioPath, []byte(`{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "cf-example-com",
      "provider": "cloudflare",
      "domain": "example.com",
      "authority": {"name_servers": ["a.ns.cloudflare.com", "b.ns.cloudflare.com"]},
      "records": [{"type": "A", "name": "@", "value": "192.0.2.10", "ttl": 30}]
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write portfolio: %v", err)
	}
	outputPath := filepath.Join(dir, "out.yaml")
	reportPath := filepath.Join(dir, "report.json")

	code := New().RunCLI([]string{"dns", "intent", "compile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", outputPath,
		"--report", reportPath,
	})
	if code != 0 {
		t.Fatalf("RunCLI intent compile exit = %d, want 0", code)
	}

	cfgData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output config: %v", err)
	}
	var cfg struct {
		Modules []struct {
			Name   string         `yaml:"name"`
			Type   string         `yaml:"type"`
			Config map[string]any `yaml:"config"`
		} `yaml:"modules"`
	}
	if err := yaml.Unmarshal(cfgData, &cfg); err != nil {
		t.Fatalf("parse output config: %v\n%s", err, cfgData)
	}
	var foundDNS bool
	for _, module := range cfg.Modules {
		if module.Name != "cf-example-com" {
			continue
		}
		foundDNS = true
		if module.Type != "infra.dns" {
			t.Fatalf("dns module type = %q", module.Type)
		}
		records, ok := module.Config["records"].([]any)
		if !ok || len(records) != 1 {
			t.Fatalf("records = %#v, want one record", module.Config["records"])
		}
		rec, ok := records[0].(map[string]any)
		if !ok || rec["ttl"] != 60 {
			t.Fatalf("record = %#v, want ttl normalized to 60", records[0])
		}
	}
	if !foundDNS {
		t.Fatalf("generated config missing cf-example-com: %#v", cfg.Modules)
	}

	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var report struct {
		Schema         string `json:"schema"`
		BlockedDomains int    `json:"blocked_domains"`
		ActionCount    int    `json:"action_count"`
	}
	if err := json.Unmarshal(reportData, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, reportData)
	}
	if report.Schema != "workflow.domain-intent.report.v1" || report.BlockedDomains != 0 || report.ActionCount != 1 {
		t.Fatalf("bad report: %+v", report)
	}
}

func TestRunCLIIntentCompileStdoutIsMachineReadableWhenOutputIsDash(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
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
	portfolioPath := writeFile(t, dir, "portfolio.json", `{
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
	reportPath := filepath.Join(dir, "report.json")

	stdout := captureStdout(t, func() {
		code := New().RunCLI([]string{"dns", "intent", "compile",
			"--intent", intentPath,
			"--portfolio", portfolioPath,
			"--output", "-",
			"--report", reportPath,
		})
		if code != 0 {
			t.Fatalf("RunCLI compile to stdout exit = %d, want 0", code)
		}
	})
	if strings.Contains(stdout, "example.com:") {
		t.Fatalf("stdout contains human summary and would corrupt YAML:\n%s", stdout)
	}
	if !strings.Contains(stdout, "modules:") || !strings.Contains(stdout, "cf-example-com") {
		t.Fatalf("stdout missing generated YAML:\n%s", stdout)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = oldStdout
	}()
	fn()
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	data, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

type yamlTestModule struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Config map[string]any `yaml:"config"`
}

func yamlModuleByName(modules []yamlTestModule, name string) *yamlTestModule {
	for i := range modules {
		if modules[i].Name == name {
			return &modules[i]
		}
	}
	return nil
}
