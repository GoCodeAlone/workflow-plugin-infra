package dnscli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/intent"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/record"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/stage"
	"gopkg.in/yaml.v3"
)

type CLI struct {
	runCommand func(name string, args ...string) error
}

func New() *CLI {
	return &CLI{runCommand: runExternalCommand}
}

func (c *CLI) RunCLI(args []string) int {
	if err := c.run(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func (c *CLI) run(args []string) error {
	if len(args) < 1 || args[0] != "dns" {
		return fmt.Errorf("usage: wfctl dns <subcommand> [flags]")
	}
	return c.runDNS(args[1:])
}

func (c *CLI) runDNS(args []string) error {
	if len(args) < 1 {
		return dnsUsage()
	}
	switch args[0] {
	case "intent":
		return c.runDNSIntent(args[1:])
	case "stage":
		return c.runDNSStage(args[1:])
	case "-h", "--help", "help":
		return dnsUsageHelp()
	default:
		return fmt.Errorf("dns: unknown subcommand %q", args[0])
	}
}

func dnsUsage() error {
	fmt.Fprint(os.Stderr, `Usage: wfctl dns <subcommand> [flags]

DNS orchestration helpers.

Subcommands:
  intent   Compile and reconcile domain intent into infra resources
  stage    Compile provider staging resources from DNS portfolios
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func dnsUsageHelp() error {
	_ = dnsUsage()
	return flag.ErrHelp
}

func (c *CLI) runDNSIntent(args []string) error {
	if len(args) < 1 {
		return dnsIntentUsage()
	}
	switch args[0] {
	case "compile":
		return c.runDNSIntentCompile(args[1:])
	case "reconcile":
		return c.runDNSIntentReconcile(args[1:])
	case "-h", "--help", "help":
		return dnsIntentUsageHelp()
	default:
		return fmt.Errorf("dns intent: unknown subcommand %q", args[0])
	}
}

func dnsIntentUsage() error {
	fmt.Fprint(os.Stderr, `Usage: wfctl dns intent <subcommand> [flags]

Subcommands:
  compile     Compile domain intent JSON and DNS portfolio exports
  reconcile   Compile, validate, and plan/apply domain intent
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func dnsIntentUsageHelp() error {
	_ = dnsIntentUsage()
	return flag.ErrHelp
}

func (c *CLI) runDNSStage(args []string) error {
	if len(args) < 1 {
		return dnsStageUsage()
	}
	switch args[0] {
	case "cloudflare":
		return c.runDNSStageCloudflare(args[1:])
	case "-h", "--help", "help":
		return dnsStageUsageHelp()
	default:
		return fmt.Errorf("dns stage: unknown subcommand %q", args[0])
	}
}

func dnsStageUsage() error {
	fmt.Fprint(os.Stderr, `Usage: wfctl dns stage <provider> [flags]

Subcommands:
  cloudflare   Compile Cloudflare infra.dns staging resources from DNS portfolios
`)
	return fmt.Errorf("missing or unknown subcommand")
}

func dnsStageUsageHelp() error {
	_ = dnsStageUsage()
	return flag.ErrHelp
}

func (c *CLI) runDNSStageCloudflare(args []string) error {
	opts, err := parseStageCloudflareOptions(args)
	if err != nil {
		return err
	}
	bundle, err := stage.CompileCloudflare(stage.CloudflareOptions{
		PortfolioGlobs: splitCSV(opts.portfolioCSV),
		Scope:          opts.scope,
		DomainFilter:   opts.domain,
		StateDir:       opts.stateDir,
	})
	if err != nil {
		return err
	}
	configBytes, err := yaml.Marshal(bundle.Config)
	if err != nil {
		return fmt.Errorf("marshal generated config: %w", err)
	}
	reportBytes, err := json.MarshalIndent(bundle.Report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	if err := writeFileCreatingParents(opts.outputPath, configBytes); err != nil {
		return err
	}
	if err := writeFileCreatingParents(opts.reportPath, reportBytes); err != nil {
		return err
	}
	if opts.bundlePath != "" {
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal bundle: %w", err)
		}
		bundleBytes = append(bundleBytes, '\n')
		if err := writeFileCreatingParents(opts.bundlePath, bundleBytes); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Selected domains: %d / %d (scope=%s)\n", bundle.Report.SelectedDomains, bundle.Report.TotalCatalogDomains, bundle.Report.Scope)
	return nil
}

func (c *CLI) runDNSIntentCompile(args []string) error {
	opts, err := parseCompileOptions("dns intent compile", args)
	if err != nil {
		return err
	}
	bundle, err := compileDNSIntentBundle(opts)
	if err != nil {
		return err
	}
	if bundle.Report.BlockedDomains > 0 {
		return fmt.Errorf("%d domain(s) blocked", bundle.Report.BlockedDomains)
	}
	return nil
}

func (c *CLI) runDNSIntentReconcile(args []string) error {
	opts, err := parseReconcileOptions(args)
	if err != nil {
		return err
	}
	switch opts.mode {
	case "plan":
	case "apply":
		if !opts.autoApprove {
			return fmt.Errorf("--mode apply requires --auto-approve")
		}
	default:
		return fmt.Errorf("unsupported reconcile mode %q (want plan or apply)", opts.mode)
	}
	bundle, err := compileDNSIntentBundle(opts.compileOptions)
	if err != nil {
		return err
	}
	if bundle.Report.BlockedDomains > 0 {
		return fmt.Errorf("%d domain(s) blocked", bundle.Report.BlockedDomains)
	}
	if bundle.Report.ActionCount == 0 && !opts.allowEmpty {
		return fmt.Errorf("domain intent produced no actions; use --allow-empty to accept a no-op")
	}
	validateArgs := []string{"validate", "--allow-no-entry-points"}
	if opts.pluginDir != "" {
		validateArgs = append(validateArgs, "--plugin-dir", opts.pluginDir)
	}
	validateArgs = append(validateArgs, opts.outputPath)
	if err := c.runCommand(wfctlBinary(), validateArgs...); err != nil {
		return fmt.Errorf("validate generated domain intent config: %w", err)
	}
	if opts.mode == "apply" && opts.verifyDelegation {
		if err := c.verifyDelegation(bundle.Report, opts, "before"); err != nil {
			return err
		}
	}
	planArgs := []string{"infra", "plan", "--config", opts.outputPath}
	if opts.pluginDir != "" {
		planArgs = append(planArgs, "--plugin-dir", opts.pluginDir)
	}
	if opts.planPath != "" {
		planArgs = append(planArgs, "--output", opts.planPath)
	}
	if err := c.runCommand(wfctlBinary(), planArgs...); err != nil {
		return fmt.Errorf("plan domain intent: %w", err)
	}
	if opts.mode != "apply" {
		return nil
	}
	applyArgs := []string{"infra", "apply", "--config", opts.outputPath, "--auto-approve"}
	if opts.pluginDir != "" {
		applyArgs = append(applyArgs, "--plugin-dir", opts.pluginDir)
	}
	if err := c.runCommand(wfctlBinary(), applyArgs...); err != nil {
		return fmt.Errorf("apply domain intent: %w", err)
	}
	if opts.verifyDelegation {
		if err := c.verifyDelegation(bundle.Report, opts, "after"); err != nil {
			return err
		}
	}
	return nil
}

type compileOptions struct {
	intentPath   string
	portfolioCSV string
	domain       string
	outputPath   string
	reportPath   string
	bundlePath   string
	stateDir     string
}

type reconcileOptions struct {
	compileOptions
	planPath    string
	pluginDir   string
	mode        string
	autoApprove bool
	allowEmpty  bool

	verifyDelegation     bool
	delegationConfigDir  string
	delegationBeforePath string
	delegationAfterPath  string
}

type stageCloudflareOptions struct {
	portfolioCSV string
	scope        string
	domain       string
	outputPath   string
	reportPath   string
	bundlePath   string
	stateDir     string
}

func parseCompileOptions(name string, args []string) (compileOptions, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var opts compileOptions
	fs.StringVar(&opts.intentPath, "intent", "domains.json", "Domain intent JSON file")
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.domain, "domain", "", "Optional single domain to compile")
	fs.StringVar(&opts.outputPath, "output", "infra/domain-reconcile.generated.wfctl.yaml", "Generated wfctl config path")
	fs.StringVar(&opts.reportPath, "report", "reports/domain-reconcile-report.json", "Generated JSON report path")
	fs.StringVar(&opts.bundlePath, "bundle", "", "Optional combined JSON bundle path")
	fs.StringVar(&opts.stateDir, "state-dir", ".state/domain-intent/", "Filesystem state directory for generated iac.state")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("%s: unexpected positional argument(s): %s", name, strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parseReconcileOptions(args []string) (reconcileOptions, error) {
	fs := flag.NewFlagSet("dns intent reconcile", flag.ContinueOnError)
	var opts reconcileOptions
	fs.StringVar(&opts.intentPath, "intent", "domains.json", "Domain intent JSON file")
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.domain, "domain", "", "Optional single domain to reconcile")
	fs.StringVar(&opts.outputPath, "output", "infra/domain-reconcile.generated.wfctl.yaml", "Generated wfctl config path")
	fs.StringVar(&opts.reportPath, "report", "reports/domain-reconcile-report.json", "Generated JSON report path")
	fs.StringVar(&opts.bundlePath, "bundle", "", "Optional combined JSON bundle path")
	fs.StringVar(&opts.stateDir, "state-dir", ".state/domain-intent/", "Filesystem state directory for generated iac.state")
	fs.StringVar(&opts.planPath, "plan-output", "reports/domain-reconcile-plan.json", "Generated infra plan JSON path")
	fs.StringVar(&opts.pluginDir, "plugin-dir", "", "Plugin directory passed to validate/plan/apply")
	fs.StringVar(&opts.mode, "mode", "plan", "Reconcile mode: plan or apply")
	fs.BoolVar(&opts.autoApprove, "auto-approve", false, "Pass --auto-approve to infra apply (required with --mode apply)")
	fs.BoolVar(&opts.allowEmpty, "allow-empty", false, "Allow intent with zero generated actions")
	fs.BoolVar(&opts.verifyDelegation, "verify-delegation", false, "For apply mode, import registrar delegation before and after apply and verify expected/desired nameservers")
	fs.StringVar(&opts.delegationConfigDir, "delegation-config-dir", "infra", "Directory containing <provider>.wfctl.yaml configs for delegation verification imports")
	fs.StringVar(&opts.delegationBeforePath, "delegation-before-output", "reports/domain-reconcile-delegation-before.json", "Registrar delegation portfolio output before apply")
	fs.StringVar(&opts.delegationAfterPath, "delegation-after-output", "reports/domain-reconcile-delegation-after.json", "Registrar delegation portfolio output after apply")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("dns intent reconcile: unexpected positional argument(s): %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func parseStageCloudflareOptions(args []string) (stageCloudflareOptions, error) {
	fs := flag.NewFlagSet("dns stage cloudflare", flag.ContinueOnError)
	var opts stageCloudflareOptions
	fs.StringVar(&opts.portfolioCSV, "portfolio", "zones/*.portfolio.json", "Comma-separated DNS portfolio JSON paths or globs")
	fs.StringVar(&opts.scope, "scope", "safe", "Stage scope: safe or all")
	fs.StringVar(&opts.domain, "domain", "", "Optional single domain to stage")
	fs.StringVar(&opts.outputPath, "output", "infra/cloudflare-staging.generated.wfctl.yaml", "Generated wfctl config path")
	fs.StringVar(&opts.reportPath, "report", "reports/cloudflare-staging-report.json", "Generated JSON report path")
	fs.StringVar(&opts.bundlePath, "bundle", "", "Optional combined JSON bundle path")
	fs.StringVar(&opts.stateDir, "state-dir", ".state/cloudflare-staging/", "Filesystem state directory for generated iac.state")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("dns stage cloudflare: unexpected positional argument(s): %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func (c *CLI) verifyDelegation(report intent.Report, opts reconcileOptions, phase string) error {
	providers := delegationProviders(report)
	if len(providers) == 0 {
		fmt.Fprintf(os.Stderr, "No registrar delegation actions; skipping %s delegation verification.\n", phase)
		return nil
	}
	sort.Strings(providers)
	multipleProviders := len(providers) > 1
	for _, provider := range providers {
		outputPath := opts.delegationBeforePath
		if phase == "after" {
			outputPath = opts.delegationAfterPath
		}
		outputPath = providerOutputPath(outputPath, provider, multipleProviders)
		args := []string{
			"infra", "import-all",
			"--config", filepath.Join(opts.delegationConfigDir, provider+".wfctl.yaml"),
			"--provider", provider,
			"--type", "infra.dns_delegation",
			"--format", "portfolio",
		}
		if opts.pluginDir != "" {
			args = append(args, "--plugin-dir", opts.pluginDir)
		}
		args = append(args, "-o", outputPath)
		if err := c.runCommand(wfctlBinary(), args...); err != nil {
			return fmt.Errorf("import %s registrar delegation for %s: %w", phase, provider, err)
		}
		portfolio, err := loadDelegationPortfolio(outputPath)
		if err != nil {
			return err
		}
		if err := verifyDelegationPortfolio(report, provider, portfolio, phase); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Registrar delegation %s verification matched intent.\n", phase)
	return nil
}

func delegationProviders(report intent.Report) []string {
	seen := map[string]bool{}
	for _, domain := range report.Domains {
		for _, action := range domain.Actions {
			if action.Type == "set_nameservers" && action.Provider != "" {
				seen[action.Provider] = true
			}
		}
	}
	providers := make([]string, 0, len(seen))
	for provider := range seen {
		providers = append(providers, provider)
	}
	return providers
}

func providerOutputPath(path, provider string, multipleProviders bool) string {
	if !multipleProviders || path == "" || path == "-" {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + "-" + provider + ext
}

func loadDelegationPortfolio(path string) (*record.Portfolio, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read delegation import %q: %w", path, err)
	}
	var portfolio record.Portfolio
	if err := json.Unmarshal(data, &portfolio); err != nil {
		return nil, fmt.Errorf("parse delegation import %q: %w", path, err)
	}
	if err := portfolio.Validate(); err != nil {
		return nil, fmt.Errorf("delegation import %q: %w", path, err)
	}
	return &portfolio, nil
}

func verifyDelegationPortfolio(report intent.Report, provider string, portfolio *record.Portfolio, phase string) error {
	for _, domain := range report.Domains {
		for _, action := range domain.Actions {
			if action.Type != "set_nameservers" || action.Provider != provider {
				continue
			}
			want := action.ExpectedCurrentNameservers
			if phase == "after" {
				want = action.DesiredNameservers
			}
			if len(want) == 0 {
				continue
			}
			got, ok := portfolioNameservers(portfolio, domain.Domain)
			if !ok {
				return fmt.Errorf("%s missing from %s registrar delegation import for %s", domain.Domain, phase, provider)
			}
			want = normalizeNameserverSet(want)
			if !equalStringSlices(got, want) {
				return fmt.Errorf("%s %s registrar nameservers %q did not match expected %q", domain.Domain, phase, strings.Join(got, ","), strings.Join(want, ","))
			}
		}
	}
	return nil
}

func portfolioNameservers(portfolio *record.Portfolio, domain string) ([]string, bool) {
	normalizedDomain := normalizeDomainName(domain)
	for _, snapshot := range portfolio.Snapshots {
		if normalizeDomainName(snapshot.Domain) != normalizedDomain {
			continue
		}
		ns := nameserversFromAuthority(snapshot.Authority, "registrar_nameservers")
		if len(ns) == 0 {
			ns = nameserversFromAuthority(snapshot.Authority, "live_nameservers")
		}
		ns = normalizeNameserverSet(ns)
		return ns, len(ns) > 0
	}
	return nil, false
}

func nameserversFromAuthority(authority map[string]any, key string) []string {
	if authority == nil {
		return nil
	}
	switch value := authority[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeNameserverSet(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		normalized := normalizeDomainName(value)
		if normalized != "" {
			seen[normalized] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeDomainName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func equalStringSlices(a, b []string) bool {
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

func compileDNSIntentBundle(opts compileOptions) (*intent.Bundle, error) {
	bundle, err := intent.Compile(intent.Options{
		IntentPath:     opts.intentPath,
		PortfolioGlobs: splitCSV(opts.portfolioCSV),
		DomainFilter:   opts.domain,
		StateDir:       opts.stateDir,
	})
	if err != nil {
		return nil, err
	}
	configBytes, err := yaml.Marshal(bundle.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal generated config: %w", err)
	}
	reportBytes, err := json.MarshalIndent(bundle.Report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}
	reportBytes = append(reportBytes, '\n')
	if err := writeFileCreatingParents(opts.outputPath, configBytes); err != nil {
		return nil, err
	}
	if err := writeFileCreatingParents(opts.reportPath, reportBytes); err != nil {
		return nil, err
	}
	if opts.bundlePath != "" {
		bundleBytes, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal bundle: %w", err)
		}
		bundleBytes = append(bundleBytes, '\n')
		if err := writeFileCreatingParents(opts.bundlePath, bundleBytes); err != nil {
			return nil, err
		}
	}
	printSummary(bundle)
	return bundle, nil
}

func printSummary(bundle *intent.Bundle) {
	for i := range bundle.Report.Domains {
		domainReport := &bundle.Report.Domains[i]
		if len(domainReport.Blockers) == 0 {
			actionTypes := make([]string, 0, len(domainReport.Actions))
			for j := range domainReport.Actions {
				actionTypes = append(actionTypes, domainReport.Actions[j].Type)
			}
			fmt.Fprintf(os.Stderr, "%s: %s\n", domainReport.Domain, strings.Join(actionTypes, ","))
		} else {
			fmt.Fprintf(os.Stderr, "%s: blocked: %s\n", domainReport.Domain, strings.Join(domainReport.Blockers, "; "))
		}
	}
}

func writeFileCreatingParents(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func wfctlBinary() string {
	if value := strings.TrimSpace(os.Getenv("WFCTL_BIN")); value != "" {
		return value
	}
	return "wfctl"
}

func runExternalCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // command is wfctl or explicit WFCTL_BIN operator override.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
