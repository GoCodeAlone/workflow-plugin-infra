// Command workflow-plugin-infra is a workflow engine external plugin that
// provides abstract infra.* module types with IaCProvider delegation.
//
// Post-Phase-3b (workflow-plugin-infra v1.0.0): no step types are registered.
// DNS intent orchestration is exposed as the plugin-owned `wfctl dns` command;
// the cross-cutting DNS policy gate remains in wfctl because infra apply owns
// that lifecycle hook.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-infra/internal"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnscli"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.ServePluginFull(
		internal.NewInfraPlugin(),
		dnscli.New(),
		nil, // no hook handler
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
	)
}
