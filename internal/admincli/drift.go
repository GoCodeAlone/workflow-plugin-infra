package admincli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsprovider"
)

// drift implements: wfctl infra-dns drift <zone> [flags]
// Fetches live TXT policy and prints the parsed entries + any structural warnings.
func drift(args []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	provider := fs.String("provider", "digitalocean", "DNS provider (digitalocean|cloudflare)")
	token := fs.String("token", "", "API token (or set via env $DNS_TOKEN)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns drift <zone> [flags]")
		fmt.Fprintln(os.Stderr, "  zone argument is required")
		fs.PrintDefaults()
		return 2
	}
	zone := fs.Arg(0)

	tok := *token
	if tok == "" {
		tok = os.Getenv("DNS_TOKEN")
	}
	if tok == "" {
		fmt.Fprintln(os.Stderr, "drift: --token or $DNS_TOKEN required")
		return 2
	}

	adapter, err := dnsprovider.NewAdapter(*provider, map[string]string{"token": tok})
	if err != nil {
		fmt.Fprintf(os.Stderr, "drift: %v\n", err)
		return 1
	}

	ctx := context.Background()
	policyName := "_workflow-dns-policy." + zone
	rrs, err := adapter.GetTXT(ctx, policyName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drift: fetch policy: %v\n", err)
		return 1
	}

	if len(rrs) == 0 {
		fmt.Printf("drift: no policy found at %s\n", policyName)
		return 0
	}

	pol, err := dnspolicy.Parse(zone, rrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drift: parse error: %v\n", err)
		return 1
	}

	fmt.Printf("Zone: %s\n", zone)
	fmt.Printf("Policy TXT: %s\n", policyName)
	fmt.Printf("Entries (%d):\n", len(pol.Entries))

	// Sort for deterministic output
	entries := append([]dnspolicy.Entry(nil), pol.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Owner < entries[j].Owner })

	for _, e := range entries {
		defaultMark := ""
		if e.Default {
			defaultMark = " [default]"
		}
		patterns := "*"
		if len(e.Patterns) > 0 {
			patterns = strings.Join(e.Patterns, ", ")
		}
		typeList := "all (except SOA/NS)"
		if len(e.Types) > 0 {
			typeList = strings.Join(e.Types, ", ")
		}
		fmt.Printf("  owner=%-20s patterns=%-30s types=%s%s\n",
			e.Owner, patterns, typeList, defaultMark)
	}
	return 0
}
