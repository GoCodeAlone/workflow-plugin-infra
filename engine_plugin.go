// Package workflowplugininfra provides the workflow-plugin-infra entry points.
//
// Two complementary shapes coexist (ADR 0056 dual-shape):
//   - NewInfraPlugin() (internal.NewInfraPlugin) is the external gRPC/sdk
//     sdk.PluginProvider — it serves infra.* module types and returns
//     StepTypes() = nil (no sdk steps). UNCHANGED by this file.
//   - NewInfraEnginePlugin() (this file) is the in-process plugin.EnginePlugin.
//     It exposes infra's first stepTypes — the four secret-admin steps — for
//     in-process consumption (e.g., by ratchet, a modular app) WITHOUT spawning
//     a gRPC subprocess.
//
// The four secret-admin steps are written NATIVELY as plugin.StepFactory
// (in-process signature: Execute(ctx, *interfaces.PipelineContext) -> *interfaces.StepResult).
// No reverse sdk→in-process bridge is required (unlike workflow-plugin-auth's
// pre-existing sdk steps, which were authored against the sdk signature and
// need wrapping).
package workflowplugininfra

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
)

// InfraEnginePlugin is the in-process EnginePlugin entry for infra. It exposes
// the four secret-admin step types (step.secret_list, step.secret_delete,
// step.secret_vault_status, step.secret_vault_test) for in-process consumption.
// The gRPC path (NewInfraPlugin) is unchanged and serves no step types.
type InfraEnginePlugin struct {
	plugin.BaseEnginePlugin
}

// NewInfraEnginePlugin returns an in-process plugin.EnginePlugin that exposes
// infra's secret-admin steps natively (ADR 0056 dual-shape). The four steps
// are in-process-only: they are NOT declared in plugin.json capabilities.stepTypes
// (the gRPC binary does not serve them — declaring them would cause a
// verify-capabilities declared-vs-served mismatch). They are cataloged in the
// manifest description instead.
func NewInfraEnginePlugin() plugin.EnginePlugin {
	return &InfraEnginePlugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "workflow-plugin-infra",
				PluginVersion:     internal.Version,
				PluginDescription: "IaC + secret-admin plugin (in-process): infra.* module types, secret-admin steps",
			},
			Manifest: plugin.PluginManifest{
				Name:        "workflow-plugin-infra",
				Version:     internal.Version,
				Author:      "GoCodeAlone",
				Description: "Plugin-owned infra.* module types + in-process secret-admin steps (secret_list/secret_delete/secret_vault_status/secret_vault_test)",
				ModuleTypes: []string{"infra.http_redirect", "infra.admin"},
				StepTypes: []string{
					"step.secret_list",
					"step.secret_delete",
					"step.secret_vault_status",
					"step.secret_vault_test",
				},
			},
		},
	}
}

// StepFactories returns the four secret-admin step factories as NATIVE
// in-process plugin.StepFactory values. Each factory's returned step implements
// interfaces.PipelineStep (Execute(ctx, *interfaces.PipelineContext)).
//
// plugin.StepFactory returns (any, error); the internal factories return
// (interfaces.PipelineStep, error). A thin closure widens the concrete return
// type to any (Go does not implicitly widen function return types).
func (p *InfraEnginePlugin) StepFactories() map[string]plugin.StepFactory {
	widen := func(f func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error)) plugin.StepFactory {
		return func(name string, config map[string]any, app modular.Application) (any, error) {
			return f(name, config, app)
		}
	}
	return map[string]plugin.StepFactory{
		"step.secret_list":         widen(internal.NewSecretListStepFactory()),
		"step.secret_delete":       widen(internal.NewSecretDeleteStepFactory()),
		"step.secret_vault_status": widen(internal.NewSecretVaultStatusStepFactory()),
		"step.secret_vault_test":   widen(internal.NewSecretVaultTestStepFactory()),
	}
}
