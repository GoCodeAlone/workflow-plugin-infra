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
