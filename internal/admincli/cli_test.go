package admincli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// runAndCapture calls RunCLI with the given args and captures combined stdout+stderr output.
func runAndCapture(args []string) (int, string) {
	// Redirect stderr temporarily
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cli := &CLIProvider{}
	code := cli.RunCLI(args)

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return code, buf.String()
}

func TestTransferOwnership_RejectsSelfTransfer(t *testing.T) {
	// dispatch with --from=sre --to=sre, expect exit code 2 + stderr contains "must differ".
	// Note: Go's flag package stops at the first non-flag arg, so positional args
	// must come after all flags.
	code, out := runAndCapture([]string{"infra-dns", "transfer-ownership",
		"--from=sre", "--to=sre", "--token=dummy", "example.com"})
	if code != 2 {
		t.Errorf("self-transfer: want exit code 2, got %d (out=%q)", code, out)
	}
	if !strings.Contains(out, "must differ") {
		t.Errorf("self-transfer: want stderr to contain 'must differ', got %q", out)
	}
}

func TestCLIProvider_DispatchSubcommands(t *testing.T) {
	cases := []struct {
		args       []string
		wantCode   int
		wantOutSub string
	}{
		{[]string{"infra-dns"}, 2, "usage"},             // no subcommand
		{[]string{"infra-dns", "unknown"}, 2, "unknown"}, // unknown subcommand
		{[]string{"infra-dns", "set-policy"}, 2, "zone"}, // missing zone arg
		{[]string{"infra-dns", "policy", "show"}, 2, "zone"}, // missing zone arg for show
	}
	for _, c := range cases {
		code, out := runAndCapture(c.args)
		if code != c.wantCode {
			t.Errorf("args=%v code=%d want %d (out=%q)", c.args, code, c.wantCode, out)
		}
		if !strings.Contains(strings.ToLower(out), c.wantOutSub) {
			t.Errorf("args=%v out=%q want substring %q", c.args, out, c.wantOutSub)
		}
	}
}
