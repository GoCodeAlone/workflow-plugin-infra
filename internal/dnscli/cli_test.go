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
        {"type": "TXT", "name": "@", "value": "google-site-verification=abc123", "ttl": 900},
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
	if !ok || len(records) != 4 || !hasYAMLManagedMarker(records) {
		t.Fatalf("records = %#v, want A+MX+TXT plus managed marker", dns.Config["records"])
	}
	if !hasYAMLRecordData(records, "TXT", "@", `"google-site-verification=abc123"`) {
		t.Fatalf("TXT records = %#v, want quoted google-site-verification data", records)
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

func TestRunDNSIntentReconcileApplyVerifiesDelegation(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": false,
      "nameserver_cutover": true,
      "expected_current_nameservers": ["ns2.hover.com", "ns1.hover.com"]
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
      "authority": {"name_servers": ["Amos.NS.Cloudflare.com.", "mckinley.ns.cloudflare.com"]}
    },
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]}
    }
  ]
}`)
	outputPath := filepath.Join(dir, "out.yaml")
	reportPath := filepath.Join(dir, "report.json")
	planPath := filepath.Join(dir, "plan.json")
	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")
	pluginDir := filepath.Join(dir, "plugins")
	configDir := filepath.Join(dir, "infra")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	writeFile(t, configDir, "hover.wfctl.yaml", "modules: []\n")

	var calls [][]string
	cli := &CLI{
		runCommand: func(name string, args ...string) error {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			if len(args) > 0 && args[0] == "infra" && len(args) > 1 && args[1] == "import-all" {
				output := argValue(args, "-o")
				if output == "" {
					t.Fatalf("import-all call missing -o: %#v", call)
				}
				body := `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns2.hover.com", "ns1.hover.com"]}
    }
  ]
}`
				if output == afterPath {
					body = `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["mckinley.ns.cloudflare.com.", "amos.ns.cloudflare.com."]}
    }
  ]
}`
				}
				if err := os.WriteFile(output, []byte(body), 0o600); err != nil {
					t.Fatalf("write import output: %v", err)
				}
			}
			return nil
		},
	}

	code := cli.RunCLI([]string{"dns", "intent", "reconcile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", outputPath,
		"--report", reportPath,
		"--plan-output", planPath,
		"--plugin-dir", pluginDir,
		"--mode", "apply",
		"--auto-approve",
		"--verify-delegation",
		"--delegation-config-dir", configDir,
		"--delegation-before-output", beforePath,
		"--delegation-after-output", afterPath,
	})
	if code != 0 {
		t.Fatalf("RunCLI reconcile apply exit = %d, want 0", code)
	}
	want := [][]string{
		{"wfctl", "validate", "--allow-no-entry-points", "--plugin-dir", pluginDir, outputPath},
		{"wfctl", "infra", "import-all", "--config", filepath.Join(configDir, "hover.wfctl.yaml"), "--provider", "hover", "--type", "infra.dns_delegation", "--format", "portfolio", "--plugin-dir", pluginDir, "-o", beforePath},
		{"wfctl", "infra", "plan", "--config", outputPath, "--plugin-dir", pluginDir, "--output", planPath},
		{"wfctl", "infra", "apply", "--config", outputPath, "--auto-approve", "--plugin-dir", pluginDir},
		{"wfctl", "infra", "import-all", "--config", filepath.Join(configDir, "hover.wfctl.yaml"), "--provider", "hover", "--type", "infra.dns_delegation", "--format", "portfolio", "--plugin-dir", pluginDir, "-o", afterPath},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("runner calls = %#v, want %#v", calls, want)
	}
}

func TestRunDNSIntentReconcileApplyVerifiesLiveDelegation(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": false,
      "nameserver_cutover": true
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
      "authority": {"name_servers": ["Amos.NS.Cloudflare.com.", "mckinley.ns.cloudflare.com"]}
    },
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]}
    }
  ]
}`)
	var calls [][]string
	cli := &CLI{
		runCommand: func(name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
		lookupNameservers: func(domain string) ([]string, error) {
			if domain != "example.com" {
				t.Fatalf("lookup domain = %q, want example.com", domain)
			}
			return []string{"mckinley.ns.cloudflare.com.", "amos.ns.cloudflare.com."}, nil
		},
	}

	code := cli.RunCLI([]string{"dns", "intent", "reconcile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", filepath.Join(dir, "out.yaml"),
		"--report", filepath.Join(dir, "report.json"),
		"--plan-output", filepath.Join(dir, "plan.json"),
		"--mode", "apply",
		"--auto-approve",
		"--verify-live-delegation",
	})
	if code != 0 {
		t.Fatalf("RunCLI reconcile apply exit = %d, want 0", code)
	}
	if len(calls) != 3 {
		t.Fatalf("runner calls = %#v, want validate/plan/apply only", calls)
	}
}

func TestRunDNSIntentReconcileApplyFailsLiveDelegationMismatch(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": false,
      "nameserver_cutover": true
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
      "authority": {"name_servers": ["amos.ns.cloudflare.com", "mckinley.ns.cloudflare.com"]}
    }
  ]
}`)
	cli := &CLI{
		runCommand: func(_ string, _ ...string) error {
			return nil
		},
		lookupNameservers: func(_ string) ([]string, error) {
			return []string{"ns1.hover.com", "ns2.hover.com"}, nil
		},
	}

	code := cli.RunCLI([]string{"dns", "intent", "reconcile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", filepath.Join(dir, "out.yaml"),
		"--report", filepath.Join(dir, "report.json"),
		"--plan-output", filepath.Join(dir, "plan.json"),
		"--mode", "apply",
		"--auto-approve",
		"--verify-live-delegation",
	})
	if code == 0 {
		t.Fatal("reconcile should fail when live nameservers do not match desired")
	}
}

func TestRunDNSIntentReconcileApplyRequiresAutoApprove(t *testing.T) {
	code := New().RunCLI([]string{"dns", "intent", "reconcile", "--mode", "apply"})
	if code == 0 {
		t.Fatal("apply without --auto-approve should fail")
	}
}

func TestRunDNSIntentReconcileFailsBlockedDomains(t *testing.T) {
	dir := t.TempDir()
	intentPath := writeFile(t, dir, "domains.json", `{
  "schema": "workflow.domain-intent.v1",
  "domains": {
    "example.com": {
      "registrar": "hover",
      "dns_host": "cloudflare",
      "stage_dns": true
    }
  }
}`)
	portfolioPath := writeFile(t, dir, "portfolio.json", `{
  "schema": "workflow.dns-portfolio.export.v1",
  "snapshots": [
    {
      "id": "hover-example-com",
      "provider": "hover",
      "domain": "example.com",
      "authority": {"registrar_nameservers": ["ns1.hover.com", "ns2.hover.com"]},
      "records": [{"type": "A", "name": "@", "value": "216.40.34.41", "ttl": 900}]
    }
  ]
}`)
	var calls [][]string
	cli := &CLI{runCommand: func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}}

	code := cli.RunCLI([]string{"dns", "intent", "reconcile",
		"--intent", intentPath,
		"--portfolio", portfolioPath,
		"--output", filepath.Join(dir, "out.yaml"),
		"--report", filepath.Join(dir, "report.json"),
	})
	if code == 0 {
		t.Fatal("blocked reconcile should fail")
	}
	if len(calls) != 0 {
		t.Fatalf("blocked reconcile ran external commands: %#v", calls)
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

func argValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
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
		if !ok || len(records) != 2 || !hasYAMLManagedMarker(records) {
			t.Fatalf("records = %#v, want one record plus managed marker", module.Config["records"])
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

func hasYAMLManagedMarker(records []any) bool {
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if ok && record["type"] == "TXT" && record["name"] == "_workflow-dns-managed" {
			return true
		}
	}
	return false
}

func hasYAMLRecordData(records []any, recordType, name, data string) bool {
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if record["type"] == recordType && record["name"] == name && record["data"] == data {
			return true
		}
	}
	return false
}
