// Package internal implements the workflow-plugin-infra external plugin,
// providing abstract infra.* module types that delegate to an IaC provider,
// plus the infra.admin module type that serves the infrastructure management SPA.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

// Version is set at build time via -ldflags
// "-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=X.Y.Z".
// Default is a bare semver so plugin loaders that validate semver accept
// unreleased dev builds; goreleaser overrides with the real release tag.
var Version = "0.0.0"

type infraModuleDefinition struct {
	typeName      string
	configMessage string
	createTyped   func(typeName, name string, config *anypb.Any) (sdk.ModuleInstance, error)
}

var infraModuleDefinitions = []infraModuleDefinition{
	typedInfraModuleDefinition("infra.container_service", "ContainerServiceConfig", &contracts.ContainerServiceConfig{}),
	typedInfraModuleDefinition("infra.k8s_cluster", "K8SClusterConfig", &contracts.K8SClusterConfig{}),
	typedInfraModuleDefinition("infra.database", "DatabaseConfig", &contracts.DatabaseConfig{}),
	typedInfraModuleDefinition("infra.cache", "CacheConfig", &contracts.CacheConfig{}),
	typedInfraModuleDefinition("infra.vpc", "VPCConfig", &contracts.VPCConfig{}),
	typedInfraModuleDefinition("infra.load_balancer", "LoadBalancerConfig", &contracts.LoadBalancerConfig{}),
	typedInfraModuleDefinition("infra.dns", "DNSConfig", &contracts.DNSConfig{}),
	typedInfraModuleDefinition("infra.registry", "RegistryConfig", &contracts.RegistryConfig{}),
	typedInfraModuleDefinition("infra.api_gateway", "APIGatewayConfig", &contracts.APIGatewayConfig{}),
	typedInfraModuleDefinition("infra.firewall", "FirewallConfig", &contracts.FirewallConfig{}),
	typedInfraModuleDefinition("infra.iam_role", "IAMRoleConfig", &contracts.IAMRoleConfig{}),
	typedInfraModuleDefinition("infra.storage", "StorageConfig", &contracts.StorageConfig{}),
	typedInfraModuleDefinition("infra.certificate", "CertificateConfig", &contracts.CertificateConfig{}),
}

// infraTypes is the complete list of abstract infrastructure resource types.
var infraTypes = moduleTypesFromDefinitions(infraModuleDefinitions)

var infraContractRegistry = buildContractRegistry(infraModuleDefinitions)

// infraAdminModuleType is the module type name for the SPA admin contribution.
const infraAdminModuleType = "infra.admin"

// infraPlugin implements sdk.PluginProvider, sdk.TypedModuleProvider, sdk.ContractProvider,
// and sdk.ConfigProvider.
type infraPlugin struct{}

// NewInfraPlugin returns a new infraPlugin instance.
func NewInfraPlugin() sdk.PluginProvider {
	return &infraPlugin{}
}

// Manifest returns plugin metadata.
func (p *infraPlugin) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "workflow-plugin-infra",
		Version:     Version,
		Author:      "GoCodeAlone",
		Description: "Abstract infra.* module types (13 types) with IaCProvider delegation",
	}
}

// ModuleTypes returns the module type names this plugin provides.
func (p *infraPlugin) ModuleTypes() []string {
	return append(append([]string(nil), infraTypes...), infraAdminModuleType)
}

// CreateModule creates a module instance of the given type.
func (p *infraPlugin) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
	if typeName == infraAdminModuleType {
		return newInfraAdminModule(name, config), nil
	}
	for _, t := range infraTypes {
		if t == typeName {
			return &infraModule{name: name, infraType: typeName, config: config}, nil
		}
	}
	return nil, fmt.Errorf("infra plugin: unknown module type %q", typeName)
}

// TypedModuleTypes returns the protobuf-typed module type names this plugin provides.
func (p *infraPlugin) TypedModuleTypes() []string {
	return p.ModuleTypes()
}

// CreateTypedModule creates a typed module instance of the given type.
func (p *infraPlugin) CreateTypedModule(typeName, name string, config *anypb.Any) (sdk.ModuleInstance, error) {
	if typeName == infraAdminModuleType {
		factory := sdk.NewTypedModuleFactory(typeName, &contracts.InfraAdminConfig{}, func(name string, cfg *contracts.InfraAdminConfig) (sdk.ModuleInstance, error) {
			c := map[string]any{
				"api_base_path": cfg.GetApiBasePath(),
				"prefix":        cfg.GetPrefix(),
			}
			if a := cfg.GetAdmin(); a != nil {
				admin := map[string]any{
					"enabled":     a.GetEnabled(),
					"module":      a.GetModule(),
					"id":          a.GetId(),
					"title":       a.GetTitle(),
					"category":    a.GetCategory(),
					"path":        a.GetPath(),
					"render_mode": a.GetRenderMode(),
					"app_context": a.GetAppContext(),
					"permissions": a.GetPermissions(),
				}
				c["admin"] = admin
			}
			return newInfraAdminModule(name, c), nil
		})
		return factory.CreateTypedModule(typeName, name, config)
	}
	for _, definition := range infraModuleDefinitions {
		if definition.typeName == typeName {
			return definition.createTyped(typeName, name, config)
		}
	}
	return nil, fmt.Errorf("infra plugin: unknown module type %q", typeName)
}

func typedInfraModuleDefinition[C proto.Message](typeName, configMessage string, configPrototype C) infraModuleDefinition {
	return infraModuleDefinition{
		typeName:      typeName,
		configMessage: configMessage,
		createTyped: func(typeName, name string, config *anypb.Any) (sdk.ModuleInstance, error) {
			factory := typedModuleFactory(typeName, configPrototype)
			return factory.CreateTypedModule(typeName, name, config)
		},
	}
}

func typedModuleFactory[C proto.Message](typeName string, configPrototype C) *sdk.TypedModuleFactory[C] {
	return sdk.NewTypedModuleFactory(typeName, configPrototype, func(name string, cfg C) (sdk.ModuleInstance, error) {
		config, err := protoMessageToMap(cfg)
		if err != nil {
			return nil, err
		}
		return &infraModule{name: name, infraType: typeName, config: config}, nil
	})
}

// StepTypes returns the step type names this plugin provides. Phase 3b
// removed the infra.dns_record step — per-record DNS workflows now route
// through `wfctl infra apply` (config-declared records) or
// `wfctl dns-policy *` (policy edits). See design doc cycle 3.5 I-NEW-1:
// step-handler peer-dispatch was architecturally unsupported.
func (p *infraPlugin) StepTypes() []string {
	return nil
}

// CreateStep creates a step instance of the given type. The plugin no
// longer registers any step types post-Phase-3b; this returns an error
// for any input.
func (p *infraPlugin) CreateStep(typeName, name string, _ map[string]any) (sdk.StepInstance, error) {
	return nil, fmt.Errorf("workflow-plugin-infra: no step types registered (was: %q)", typeName)
}

// TypedStepTypes returns the typed step type names this plugin provides.
// Empty post-Phase-3b.
func (p *infraPlugin) TypedStepTypes() []string {
	return nil
}

// CreateTypedStep creates a typed step instance. Always errors
// post-Phase-3b since this plugin no longer registers any step types.
func (p *infraPlugin) CreateTypedStep(typeName, name string, _ *anypb.Any) (sdk.StepInstance, error) {
	return nil, fmt.Errorf("workflow-plugin-infra: no typed step types registered (was: %q)", typeName)
}

// ContractRegistry returns strict protobuf descriptors for plugin module boundaries.
func (p *infraPlugin) ContractRegistry() *pb.ContractRegistry {
	return infraContractRegistry
}

func buildContractRegistry(definitions []infraModuleDefinition) *pb.ContractRegistry {
	descriptors := make([]*pb.ContractDescriptor, 0, len(definitions)+2)
	for _, definition := range definitions {
		descriptors = append(descriptors, moduleContract(definition.typeName, definition.configMessage))
	}
	// infra.admin module contract (typed config via InfraAdminConfig proto).
	descriptors = append(descriptors, &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_MODULE,
		ModuleType:    infraAdminModuleType,
		ConfigMessage: "workflow.plugins.infra.admin.v1.InfraAdminConfig",
		Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	})
	// infra.admin service method: AdminContribution (matches authz-ui pattern).
	descriptors = append(descriptors, &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_SERVICE,
		ModuleType:    infraAdminModuleType,
		ServiceName:   "InfraAdmin",
		Method:        "AdminContribution",
		InputMessage:  "workflow.plugins.infra.admin.v1.GetAdminContributionInput",
		OutputMessage: "workflow.plugins.infra.admin.v1.GetAdminContributionOutput",
		Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	})
	// No step contracts post-Phase-3b: infra.dns_record removed (peer-dispatch
	// from step-handler context infeasible; per-record workflows route through
	// wfctl infra apply or wfctl dns-policy * instead).
	return &pb.ContractRegistry{
		FileDescriptorSet: &descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{
				protodesc.ToFileDescriptorProto(structpb.File_google_protobuf_struct_proto),
				protodesc.ToFileDescriptorProto(contracts.File_internal_contracts_infra_proto),
				protodesc.ToFileDescriptorProto(contracts.File_internal_contracts_infra_admin_proto),
			},
		},
		Contracts: descriptors,
	}
}

// ConfigFragment implements sdk.ConfigProvider. It extracts the embedded SPA
// assets and returns a config fragment that wires a static.fileserver module
// to serve the infra admin SPA at /admin/infra.
func (p *infraPlugin) ConfigFragment() ([]byte, error) {
	if err := extractAssets(); err != nil {
		return nil, fmt.Errorf("infra plugin: extract assets: %w", err)
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("infra plugin: get working directory: %w", err)
	}

	absUIPath := filepath.Join(dir, "ui_dist")

	var cfg map[string]any
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		return nil, fmt.Errorf("infra plugin: parse config: %w", err)
	}

	if modules, ok := cfg["modules"].([]any); ok {
		for _, m := range modules {
			mod, ok := m.(map[string]any)
			if !ok {
				continue
			}
			modType, _ := mod["type"].(string)
			if modType == "static.fileserver" {
				if config, ok := mod["config"].(map[string]any); ok {
					config["root"] = absUIPath
				}
			}
		}
	}

	return yaml.Marshal(cfg)
}

func moduleTypesFromDefinitions(definitions []infraModuleDefinition) []string {
	types := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		types = append(types, definition.typeName)
	}
	return types
}

func moduleContract(moduleType, configMessage string) *pb.ContractDescriptor {
	const pkg = "workflow.plugins.infra.v1."
	return &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_MODULE,
		ModuleType:    moduleType,
		ConfigMessage: pkg + configMessage,
		Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
	}
}

func protoMessageToMap(msg proto.Message) (map[string]any, error) {
	if msg == nil {
		return nil, nil
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(msg)
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
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
	// Post-Phase-3b: infra.dns is once again an abstract module — the
	// previous deprecation gate (pointing to the now-removed
	// infra.dns_record step) is gone. Provisioning routes through the
	// host's resolved IaCProvider per the engine-native pattern.
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
