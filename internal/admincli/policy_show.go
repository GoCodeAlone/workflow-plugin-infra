package admincli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsgate"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsprovider"
)

// policyShow implements: wfctl infra-dns policy show <zone> [flags]
// Fetches and pretty-prints the live policy.
func policyShow(args []string) int {
	fs := flag.NewFlagSet("policy show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	provider := fs.String("provider", "digitalocean", "DNS provider (digitalocean|cloudflare)")
	token := fs.String("token", "", "API token (or set via env $DNS_TOKEN)")
	raw := fs.Bool("raw", false, "Print raw TXT RR values instead of parsed output")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns policy show <zone> [flags]")
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
		fmt.Fprintln(os.Stderr, "policy show: --token or $DNS_TOKEN required")
		return 2
	}

	adapter, err := dnsprovider.NewAdapter(*provider, map[string]string{"token": tok})
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy show: %v\n", err)
		return 1
	}

	ctx := context.Background()
	policyName := dnsgate.PolicyName(zone)
	rrs, err := adapter.GetTXT(ctx, policyName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy show: fetch: %v\n", err)
		return 1
	}

	if len(rrs) == 0 {
		fmt.Printf("No policy found at %s\n", policyName)
		return 0
	}

	if *raw {
		for _, r := range rrs {
			fmt.Println(r)
		}
		return 0
	}

	pol, err := dnspolicy.Parse(zone, rrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy show: parse: %v\n", err)
		return 1
	}

	fmt.Printf("DNS Ownership Policy for zone: %s\n", zone)
	fmt.Printf("TXT record: %s (%d RR(s))\n", policyName, len(rrs))
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
	return 0
}
