// Command workflow-plugin-infra is a workflow engine external plugin that
// provides abstract infra.* module types with IaCProvider delegation.
//
// Post-Phase-3b (workflow-plugin-infra v1.0.0): no step types are
// registered, and the legacy `infra-dns` cliCommand surface has moved to
// the wfctl `dns-policy` builtin. The plugin is now a pure abstract
// module-types provider — DNS policy + record CRUD live in the wfctl
// builtin path against any provider-plugin that implements infra.dns.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-infra/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.ServePluginFull(
		internal.NewInfraPlugin(),
		nil, // no CLI provider (commands relocated to wfctl dns-policy)
		nil, // no hook handler
		sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
	)
}
