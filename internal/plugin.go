// Package internal implements the workflow-plugin-infra external plugin,
// providing abstract infra.* module types that delegate to an IaC provider.
package internal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsaudit"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsgate"
	"github.com/GoCodeAlone/workflow-plugin-infra/internal/dnsprovider"
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

// StepTypes returns the step type names this plugin provides.
func (p *infraPlugin) StepTypes() []string {
	return []string{"infra.dns_record"}
}

// CreateStep creates a step instance of the given type.
// infra.dns_record requires typed config; direct untyped path is unsupported.
func (p *infraPlugin) CreateStep(typeName, name string, _ map[string]any) (sdk.StepInstance, error) {
	return nil, fmt.Errorf("infra.dns_record requires typed config; legacy untyped path not supported")
}

// TypedStepTypes returns the typed step type names this plugin provides.
func (p *infraPlugin) TypedStepTypes() []string {
	return []string{"infra.dns_record"}
}

// CreateTypedStep creates a typed step instance for infra.dns_record.
func (p *infraPlugin) CreateTypedStep(typeName, name string, config *anypb.Any) (sdk.StepInstance, error) {
	// cachingGate is shared across all executions of this step instance,
	// amortizing GetTXT calls when a pipeline processes many records in one
	// apply (one fetch per zone, not one per record).
	cachingGate := dnsgate.NewCachingGate()
	handler := func(ctx context.Context, req sdk.TypedStepRequest[*contracts.DNSRecordStepConfig, *contracts.DNSRecordStepInput]) (*sdk.TypedStepResult[*contracts.DNSRecordStepOutput], error) {
		creds := dnsprovider.ExpandCredsMap(req.Config.ProviderCreds)
		adapter, err := dnsprovider.NewAdapter(req.Config.Provider, creds)
		if err != nil {
			return nil, err
		}
		if gerr := cachingGate.Check(ctx, adapter, req.Config.Zone, req.Input.Name, req.Input.RecordType, req.Input.Owner); gerr != nil {
			dnsaudit.LogOutcome("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, "gate-denied", gerr.Error())
			return &sdk.TypedStepResult[*contracts.DNSRecordStepOutput]{
				Output: &contracts.DNSRecordStepOutput{Status: "gate-denied", DenialReason: gerr.Error()},
			}, nil
		}
		op := req.Input.Operation
		if op == "" {
			op = "upsert"
		}
		dnsaudit.LogAttempt("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, op, req.Input.Owner, req.Config.Provider)
		result, applyErr := dnsprovider.Apply(ctx, adapter, req.Config, req.Input)
		outcome := result.Output.Status
		errMsg := ""
		if outcome != "ok" {
			errMsg = result.Output.DenialReason
		}
		dnsaudit.LogOutcome("step-execute", req.Config.Zone, req.Input.Name, req.Input.RecordType, outcome, errMsg)
		return result, applyErr
	}
	factory := sdk.NewTypedStepFactory[*contracts.DNSRecordStepConfig, *contracts.DNSRecordStepInput, *contracts.DNSRecordStepOutput](
		typeName,
		&contracts.DNSRecordStepConfig{},
		&contracts.DNSRecordStepInput{},
		handler,
	)
	return factory.CreateTypedStep(typeName, name, config)
}

// ContractRegistry returns strict protobuf descriptors for plugin module boundaries.
func (p *infraPlugin) ContractRegistry() *pb.ContractRegistry {
	return infraContractRegistry
}

func buildContractRegistry(definitions []infraModuleDefinition) *pb.ContractRegistry {
	descriptors := make([]*pb.ContractDescriptor, 0, len(definitions)+1)
	for _, definition := range definitions {
		descriptors = append(descriptors, moduleContract(definition.typeName, definition.configMessage))
	}
	// Step contracts
	descriptors = append(descriptors, stepContract(
		"infra.dns_record",
		"DNSRecordStepConfig",
		"DNSRecordStepInput",
		"DNSRecordStepOutput",
	))
	return &pb.ContractRegistry{
		FileDescriptorSet: &descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{
				protodesc.ToFileDescriptorProto(structpb.File_google_protobuf_struct_proto),
				protodesc.ToFileDescriptorProto(contracts.File_internal_contracts_infra_proto),
			},
		},
		Contracts: descriptors,
	}
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

func stepContract(stepType, configMessage, inputMessage, outputMessage string) *pb.ContractDescriptor {
	const pkg = "workflow.plugins.infra.v1."
	return &pb.ContractDescriptor{
		Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
		StepType:      stepType,
		ConfigMessage: pkg + configMessage,
		InputMessage:  pkg + inputMessage,
		OutputMessage: pkg + outputMessage,
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
	if m.infraType == "infra.dns" {
		return fmt.Errorf("infra.dns module is deprecated; use the infra.dns_record step type instead. See docs/migration/infra-dns-to-step.md")
	}
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
