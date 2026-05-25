// Package admincli implements the wfctl infra-dns subcommand surface via sdk.CLIProvider.
package admincli

import (
	"fmt"
	"os"
)

// CLIProvider implements sdk.CLIProvider for the infra-dns command group.
type CLIProvider struct{}

// RunCLI receives args AFTER the --wfctl-cli sentinel.
// args[0] = command name ("infra-dns"); args[1:] = subcommand + flags.
func (c *CLIProvider) RunCLI(args []string) int {
	if len(args) < 1 || args[0] != "infra-dns" {
		fmt.Fprintln(os.Stderr, "admincli: expected first arg 'infra-dns'")
		return 2
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns <subcommand>\nsubcommands: set-policy, drift, transfer-ownership, policy show")
		return 2
	}
	sub := args[1]
	rest := args[2:]
	switch sub {
	case "set-policy":
		return setPolicy(rest)
	case "drift":
		return drift(rest)
	case "transfer-ownership":
		return transferOwnership(rest)
	case "policy":
		if len(rest) > 0 && rest[0] == "show" {
			return policyShow(rest[1:])
		}
		fmt.Fprintln(os.Stderr, "usage: wfctl infra-dns policy show <zone>")
		return 2
	default:
		fmt.Fprintf(os.Stderr, "admincli: unknown subcommand %q\n", sub)
		return 2
	}
}
