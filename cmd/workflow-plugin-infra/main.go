// Command workflow-plugin-infra is a workflow engine external plugin that
// provides abstract infra.* module types with IaCProvider delegation.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-infra/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

func main() {
	sdk.Serve(internal.NewInfraPlugin())
}
