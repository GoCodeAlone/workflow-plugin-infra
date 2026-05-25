// Command workflow-plugin-infra is a workflow engine external plugin that
// provides abstract infra.* module types with IaCProvider delegation,
// plus the infra.dns_record step type with DNS ownership gate enforcement.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-infra/internal"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/admincli"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.ServePluginFull(
		internal.NewInfraPlugin(),
		&admincli.CLIProvider{},
		nil, // no hook handler
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
	)
}
