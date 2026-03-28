// Package internal implements the workflow-plugin-infra external plugin,
// providing abstract infra.* module types that delegate to an IaC provider.
package internal

import (
	"context"
	"fmt"

	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// infraTypes is the complete list of abstract infrastructure resource types.
var infraTypes = []string{
	"infra.container_service",
	"infra.k8s_cluster",
	"infra.database",
	"infra.cache",
	"infra.vpc",
	"infra.load_balancer",
	"infra.dns",
	"infra.registry",
	"infra.api_gateway",
	"infra.firewall",
	"infra.iam_role",
	"infra.storage",
	"infra.certificate",
}

// infraPlugin implements sdk.PluginProvider.
type infraPlugin struct{}

// NewInfraPlugin returns a new infraPlugin instance.
func NewInfraPlugin() sdk.PluginProvider {
	return &infraPlugin{}
}

// Manifest returns plugin metadata.
func (p *infraPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "workflow-plugin-infra",
		Version:     "0.1.0",
		Author:      "GoCodeAlone",
		Description: "Abstract infra.* module types (13 types) with IaCProvider delegation",
	}
}

// ModuleTypes returns the module type names this plugin provides.
func (p *infraPlugin) ModuleTypes() []string {
	return infraTypes
}

// CreateModule creates a module instance of the given type.
func (p *infraPlugin) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
	for _, t := range infraTypes {
		if t == typeName {
			return &infraModule{name: name, infraType: typeName, config: config}, nil
		}
	}
	return nil, fmt.Errorf("infra plugin: unknown module type %q", typeName)
}

// StepTypes returns the step type names this plugin provides.
func (p *infraPlugin) StepTypes() []string {
	return []string{}
}

// CreateStep creates a step instance of the given type.
func (p *infraPlugin) CreateStep(typeName, name string, _ map[string]any) (sdk.StepInstance, error) {
	return nil, fmt.Errorf("infra plugin: unknown step type %q", typeName)
}

// ─── Module ───────────────────────────────────────────────────────────────────

// infraModule is an abstract infrastructure resource module.
// It stores configuration and delegates provisioning to an IaC provider
// (resolved via the workflow engine's service registry at runtime).
// TODO: Implement IaCProvider resolution and ResourceDriver delegation via SDK Registry.
type infraModule struct {
	name      string
	infraType string
	config    map[string]any
	state     map[string]any
}

func (m *infraModule) Init() error {
	m.state = make(map[string]any)
	return nil
}

func (m *infraModule) Start(_ context.Context) error {
	// TODO: Resolve IaCProvider from registry and provision resource
	return nil
}

func (m *infraModule) Stop(_ context.Context) error {
	return nil
}

// GetState returns the current provisioned state of this resource.
func (m *infraModule) GetState() map[string]any {
	return m.state
}

// SetState updates the provisioned state of this resource.
func (m *infraModule) SetState(state map[string]any) {
	m.state = state
}
