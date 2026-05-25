package admincli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsaudit"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsprovider"
)

// setPolicy implements: wfctl infra-dns set-policy <zone> [--provider p] [--token t] [--owner o] [--patterns p1,p2]
func setPolicy(args []string) int {
	fs := flag.NewFlagSet("set-policy", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	provider := fs.String("provider", "digitalocean", "DNS provider (digitalocean|cloudflare)")
	token := fs.String("token", "", "API token (or set via env $DNS_TOKEN)")
	owner := fs.String("owner", "", "Owner name for this policy entry (required)")
	patterns := fs.String("patterns", "", "Comma-separated name patterns (empty = default owner)")
	types := fs.String("types", "", "Comma-separated record types (empty = all except SOA/NS)")
	defaultOwner := fs.Bool("default", false, "Mark this entry as the default owner")
	ttl := fs.Int("ttl", 300, "TTL in seconds for the policy TXT record")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns set-policy <zone> [flags]")
		fmt.Fprintln(os.Stderr, "  zone argument is required")
		fs.PrintDefaults()
		return 2
	}
	zone := fs.Arg(0)

	if *owner == "" {
		fmt.Fprintln(os.Stderr, "set-policy: --owner is required")
		return 2
	}

	tok := *token
	if tok == "" {
		tok = os.Getenv("DNS_TOKEN")
	}
	if tok == "" {
		fmt.Fprintln(os.Stderr, "set-policy: --token or $DNS_TOKEN required")
		return 2
	}

	adapter, err := dnsprovider.NewAdapter(*provider, map[string]string{"token": tok})
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-policy: %v\n", err)
		return 1
	}

	ctx := context.Background()
	policyName := "_workflow-dns-policy." + zone
	existing, err := adapter.GetTXT(ctx, policyName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-policy: fetch existing policy: %v\n", err)
		return 1
	}

	pol, err := dnspolicy.Parse(zone, existing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-policy: parse existing policy: %v\n", err)
		return 1
	}

	// Build new entry
	entry := dnspolicy.Entry{
		Owner:   *owner,
		Default: *defaultOwner,
	}
	if *patterns != "" {
		for _, p := range strings.Split(*patterns, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				entry.Patterns = append(entry.Patterns, p)
			}
		}
	}
	if *types != "" {
		for _, t := range strings.Split(*types, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				entry.Types = append(entry.Types, t)
			}
		}
	}

	// Replace or append entry
	replaced := false
	for i, e := range pol.Entries {
		if e.Owner == *owner {
			pol.Entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		pol.Entries = append(pol.Entries, entry)
	}

	newRRs, err := dnspolicy.Serialize(pol)
	if err != nil {
		fmt.Fprintf(os.Stderr, "set-policy: serialize: %v\n", err)
		return 1
	}

	// Compute SHA256 of prior + new for audit
	priorSHA := sha256Strings(existing)
	newSHA := sha256Strings(newRRs)

	if err := adapter.UpsertTXT(ctx, policyName, newRRs, *ttl); err != nil {
		fmt.Fprintf(os.Stderr, "set-policy: write policy: %v\n", err)
		return 1
	}

	dnsaudit.LogPolicyEdit("wfctl", zone, "set-policy", priorSHA, newSHA)
	fmt.Printf("set-policy: wrote %d RR(s) to %s\n", len(newRRs), policyName)
	return 0
}
