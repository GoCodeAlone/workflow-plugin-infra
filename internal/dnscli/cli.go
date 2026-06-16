package dnscli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dns/intent"
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
