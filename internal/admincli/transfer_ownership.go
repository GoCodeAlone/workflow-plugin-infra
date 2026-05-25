package admincli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsaudit"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnspolicy"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsprovider"
)

// transferOwnership implements: wfctl infra-dns transfer-ownership <zone> --from <owner> --to <new-owner>
// Moves all pattern claims from one owner to another in the live policy.
func transferOwnership(args []string) int {
	fs := flag.NewFlagSet("transfer-ownership", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	provider := fs.String("provider", "digitalocean", "DNS provider (digitalocean|cloudflare)")
	token := fs.String("token", "", "API token (or set via env $DNS_TOKEN)")
	from := fs.String("from", "", "Source owner name (required)")
	to := fs.String("to", "", "Destination owner name (required)")
	ttl := fs.Int("ttl", 300, "TTL in seconds for the updated policy TXT record")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns transfer-ownership <zone> --from <owner> --to <new-owner> [flags]")
		fmt.Fprintln(os.Stderr, "  zone argument is required")
		fs.PrintDefaults()
		return 2
	}
	zone := fs.Arg(0)

	if *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "transfer-ownership: --from and --to are required")
		return 2
	}

	tok := *token
	if tok == "" {
		tok = os.Getenv("DNS_TOKEN")
	}
	if tok == "" {
		fmt.Fprintln(os.Stderr, "transfer-ownership: --token or $DNS_TOKEN required")
		return 2
	}

	adapter, err := dnsprovider.NewAdapter(*provider, map[string]string{"token": tok})
	if err != nil {
		fmt.Fprintf(os.Stderr, "transfer-ownership: %v\n", err)
		return 1
	}

	ctx := context.Background()
	policyName := "_workflow-dns-policy." + zone
	existing, err := adapter.GetTXT(ctx, policyName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transfer-ownership: fetch policy: %v\n", err)
		return 1
	}

	pol, err := dnspolicy.Parse(zone, existing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transfer-ownership: parse policy: %v\n", err)
		return 1
	}

	// Find the source entry
	fromIdx := -1
	for i, e := range pol.Entries {
		if e.Owner == *from {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		fmt.Fprintf(os.Stderr, "transfer-ownership: owner %q not found in policy\n", *from)
		return 1
	}

	// Find or create the destination entry
	toIdx := -1
	for i, e := range pol.Entries {
		if e.Owner == *to {
			toIdx = i
			break
		}
	}

	fromEntry := pol.Entries[fromIdx]
	if toIdx >= 0 {
		// Merge patterns + types
		pol.Entries[toIdx].Patterns = append(pol.Entries[toIdx].Patterns, fromEntry.Patterns...)
		if len(fromEntry.Types) > 0 {
			pol.Entries[toIdx].Types = append(pol.Entries[toIdx].Types, fromEntry.Types...)
		}
	} else {
		pol.Entries = append(pol.Entries, dnspolicy.Entry{
			Owner:    *to,
			Patterns: fromEntry.Patterns,
			Types:    fromEntry.Types,
			Default:  false,
		})
	}

	// Remove the from entry (but preserve default flag — don't silently delete default)
	if fromEntry.Default {
		fmt.Fprintf(os.Stderr, "transfer-ownership: WARNING: %q is the default owner; removing default flag on transfer\n", *from)
	}
	pol.Entries = append(pol.Entries[:fromIdx], pol.Entries[fromIdx+1:]...)

	newRRs, err := dnspolicy.Serialize(pol)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transfer-ownership: serialize: %v\n", err)
		return 1
	}

	priorSHA := sha256Strings(existing)
	newSHA := sha256Strings(newRRs)

	if err := adapter.UpsertTXT(ctx, policyName, newRRs, *ttl); err != nil {
		fmt.Fprintf(os.Stderr, "transfer-ownership: write policy: %v\n", err)
		return 1
	}

	dnsaudit.LogPolicyEdit("wfctl", zone, "transfer-ownership:"+*from+"->"+*to, priorSHA, newSHA)
	fmt.Printf("transfer-ownership: transferred %q → %q in zone %s\n", *from, *to, zone)
	return 0
}
