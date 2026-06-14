//go:build smoke
// +build smoke

package main_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPluginBinary_Smoke(t *testing.T) {
	bin := buildPlugin(t)
	// CLI dispatch path
	out, err := exec.Command(bin, "--wfctl-cli", "dns").CombinedOutput()
	if err == nil {
		t.Errorf("expected non-zero exit on bare 'dns'")
	}
	if !strings.Contains(string(out), "Usage") {
		t.Errorf("usage missing: %s", out)
	}
}

func buildPlugin(t *testing.T) string {
	t.Helper()
	bin := "/tmp/wfi-smoke-TestPluginBinary"
	cmd := exec.Command("go", "build", "-o", bin, "github.com/GoCodeAlone/workflow-plugin-infra/cmd/workflow-plugin-infra")
	cmd.Env = append(cmd.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}
