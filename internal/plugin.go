// Package internal implements the workflow-plugin-infra external plugin,
// providing abstract infra.* module types that delegate to an IaC provider.
package internal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Version is set at build time via -ldflags
// "-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=X.Y.Z".
// Default is a bare semver so plugin loaders that validate semver accept
// unreleased dev builds; goreleaser overrides with the real release tag.
var Version = "0.0.0"

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

// infraPlugin implements sdk.PluginProvider, sdk.TypedModuleProvider, and sdk.ContractProvider.
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
	return append([]string(nil), infraTypes...)
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

// TypedModuleTypes returns the protobuf-typed module type names this plugin provides.
func (p *infraPlugin) TypedModuleTypes() []string {
	return p.ModuleTypes()
}

// CreateTypedModule creates a typed module instance of the given type.
func (p *infraPlugin) CreateTypedModule(typeName, name string, config *anypb.Any) (sdk.ModuleInstance, error) {
	switch typeName {
	case "infra.container_service":
		factory := typedModuleFactory(typeName, &contracts.ContainerServiceConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.k8s_cluster":
		factory := typedModuleFactory(typeName, &contracts.K8SClusterConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.database":
		factory := typedModuleFactory(typeName, &contracts.DatabaseConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.cache":
		factory := typedModuleFactory(typeName, &contracts.CacheConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.vpc":
		factory := typedModuleFactory(typeName, &contracts.VPCConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.load_balancer":
		factory := typedModuleFactory(typeName, &contracts.LoadBalancerConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.dns":
		factory := typedModuleFactory(typeName, &contracts.DNSConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.registry":
		factory := typedModuleFactory(typeName, &contracts.RegistryConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.api_gateway":
		factory := typedModuleFactory(typeName, &contracts.APIGatewayConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.firewall":
		factory := typedModuleFactory(typeName, &contracts.FirewallConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.iam_role":
		factory := typedModuleFactory(typeName, &contracts.IAMRoleConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.storage":
		factory := typedModuleFactory(typeName, &contracts.StorageConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	case "infra.certificate":
		factory := typedModuleFactory(typeName, &contracts.CertificateConfig{})
		return factory.CreateTypedModule(typeName, name, config)
	default:
		return nil, fmt.Errorf("infra plugin: unknown module type %q", typeName)
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

// StepTypes returns the step type names this plugin provides.
func (p *infraPlugin) StepTypes() []string {
	return []string{}
}

// CreateStep creates a step instance of the given type.
func (p *infraPlugin) CreateStep(typeName, name string, _ map[string]any) (sdk.StepInstance, error) {
	return nil, fmt.Errorf("infra plugin: unknown step type %q", typeName)
}

// ContractRegistry returns strict protobuf descriptors for plugin module boundaries.
func (p *infraPlugin) ContractRegistry() *pb.ContractRegistry {
	return &pb.ContractRegistry{
		FileDescriptorSet: &descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{
				protodesc.ToFileDescriptorProto(structpb.File_google_protobuf_struct_proto),
				protodesc.ToFileDescriptorProto(contracts.File_internal_contracts_infra_proto),
			},
		},
		Contracts: []*pb.ContractDescriptor{
			moduleContract("infra.container_service", "ContainerServiceConfig"),
			moduleContract("infra.k8s_cluster", "K8SClusterConfig"),
			moduleContract("infra.database", "DatabaseConfig"),
			moduleContract("infra.cache", "CacheConfig"),
			moduleContract("infra.vpc", "VPCConfig"),
			moduleContract("infra.load_balancer", "LoadBalancerConfig"),
			moduleContract("infra.dns", "DNSConfig"),
			moduleContract("infra.registry", "RegistryConfig"),
			moduleContract("infra.api_gateway", "APIGatewayConfig"),
			moduleContract("infra.firewall", "FirewallConfig"),
			moduleContract("infra.iam_role", "IAMRoleConfig"),
			moduleContract("infra.storage", "StorageConfig"),
			moduleContract("infra.certificate", "CertificateConfig"),
		},
	}
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
