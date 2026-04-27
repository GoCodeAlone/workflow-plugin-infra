package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-infra/internal/contracts"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestInfraPluginImplementsStrictContractProviders(t *testing.T) {
	provider := NewInfraPlugin()
	if _, ok := provider.(sdk.TypedModuleProvider); !ok {
		t.Fatal("expected TypedModuleProvider")
	}
	if _, ok := provider.(sdk.ContractProvider); !ok {
		t.Fatal("expected ContractProvider")
	}
}

func TestContractRegistryDeclaresStrictModuleContracts(t *testing.T) {
	provider := NewInfraPlugin().(sdk.ContractProvider)
	registry := provider.ContractRegistry()
	if registry == nil {
		t.Fatal("expected contract registry")
	}
	if registry.FileDescriptorSet == nil || len(registry.FileDescriptorSet.File) == 0 {
		t.Fatal("expected file descriptor set")
	}
	files, err := protodesc.NewFiles(registry.FileDescriptorSet)
	if err != nil {
		t.Fatalf("descriptor set: %v", err)
	}

	manifestContracts := loadManifestContracts(t)
	contractsByKey := map[string]*pb.ContractDescriptor{}
	for _, contract := range registry.Contracts {
		if contract.Kind != pb.ContractKind_CONTRACT_KIND_MODULE {
			t.Fatalf("unexpected contract kind %s", contract.Kind)
		}
		key := "module:" + contract.ModuleType
		contractsByKey[key] = contract
		if contract.Mode != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			t.Fatalf("%s mode = %s, want strict proto", key, contract.Mode)
		}
		if contract.ConfigMessage == "" {
			t.Fatalf("%s missing config message", key)
		}
		if _, err := files.FindDescriptorByName(protoreflect.FullName(contract.ConfigMessage)); err != nil {
			t.Fatalf("%s references unknown config message %s: %v", key, contract.ConfigMessage, err)
		}
		if want, ok := manifestContracts[key]; !ok {
			t.Fatalf("%s missing from plugin.contracts.json", key)
		} else if want.ConfigMessage != contract.ConfigMessage {
			t.Fatalf("%s manifest contract = %#v, runtime = %#v", key, want, contract)
		}
	}

	for _, moduleType := range infraTypes {
		key := "module:" + moduleType
		if _, ok := contractsByKey[key]; !ok {
			t.Fatalf("missing contract %s", key)
		}
	}
	if len(manifestContracts) != len(contractsByKey) {
		t.Fatalf("plugin.contracts.json contract count = %d, runtime = %d", len(manifestContracts), len(contractsByKey))
	}
}

func TestTypedModuleProviderValidatesTypedConfig(t *testing.T) {
	provider := NewInfraPlugin().(sdk.TypedModuleProvider)
	config, err := anypb.New(&contracts.DatabaseConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Engine:   "postgres",
	})
	if err != nil {
		t.Fatalf("pack config: %v", err)
	}
	if _, err := provider.CreateTypedModule("infra.database", "db", config); err != nil {
		t.Fatalf("CreateTypedModule: %v", err)
	}

	wrongConfig, err := anypb.New(&contracts.ContainerServiceConfig{Image: "example/app:latest"})
	if err != nil {
		t.Fatalf("pack wrong config: %v", err)
	}
	if _, err := provider.CreateTypedModule("infra.database", "db", wrongConfig); err == nil {
		t.Fatal("CreateTypedModule accepted wrong typed config")
	}
}

func TestTypedContainerServiceConfigMapsToLegacyModule(t *testing.T) {
	provider := NewInfraPlugin().(sdk.TypedModuleProvider)
	config, err := anypb.New(&contracts.ContainerServiceConfig{
		Provider: "aws",
		Region:   "us-east-1",
		Image:    "example/app:latest",
		Ports:    []int32{8080},
		Env:      map[string]string{"APP_ENV": "test"},
	})
	if err != nil {
		t.Fatalf("pack config: %v", err)
	}
	module, err := provider.CreateTypedModule("infra.container_service", "app", config)
	if err != nil {
		t.Fatalf("CreateTypedModule: %v", err)
	}
	typed, ok := module.(interface {
		TypedConfig() *contracts.ContainerServiceConfig
	})
	if !ok {
		t.Fatalf("module type = %T, want typed container service module", module)
	}
	if got := typed.TypedConfig().GetImage(); got != "example/app:latest" {
		t.Fatalf("image = %q, want example/app:latest", got)
	}
	if got := typed.TypedConfig().GetProvider(); got != "aws" {
		t.Fatalf("provider = %q, want aws", got)
	}
	wrapped, ok := module.(*sdk.TypedModuleInstance[*contracts.ContainerServiceConfig])
	if !ok {
		t.Fatalf("module type = %T, want typed module wrapper", module)
	}
	legacy, ok := wrapped.ModuleInstance.(*infraModule)
	if !ok {
		t.Fatalf("wrapped module type = %T, want *infraModule", wrapped.ModuleInstance)
	}
	if got := legacy.config["image"]; got != "example/app:latest" {
		t.Fatalf("legacy image = %#v, want example/app:latest", got)
	}
	if got := legacy.config["provider"]; got != "aws" {
		t.Fatalf("legacy provider = %#v, want aws", got)
	}
	ports, ok := legacy.config["ports"].([]any)
	if !ok || len(ports) != 1 || ports[0] != float64(8080) {
		t.Fatalf("legacy ports = %#v, want [8080]", legacy.config["ports"])
	}
	env, ok := legacy.config["env"].(map[string]any)
	if !ok || env["APP_ENV"] != "test" {
		t.Fatalf("legacy env = %#v, want APP_ENV=test", legacy.config["env"])
	}
}

type manifestContract struct {
	Mode          string `json:"mode"`
	ConfigMessage string `json:"config"`
}

func loadManifestContracts(t *testing.T) map[string]manifestContract {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "plugin.contracts.json"))
	if err != nil {
		t.Fatalf("read plugin.contracts.json: %v", err)
	}
	var manifest struct {
		Version   string `json:"version"`
		Contracts []struct {
			Kind string `json:"kind"`
			Type string `json:"type"`
			manifestContract
		} `json:"contracts"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse plugin.contracts.json: %v", err)
	}
	if manifest.Version != "v1" {
		t.Fatalf("plugin.contracts.json version = %q, want v1", manifest.Version)
	}
	contracts := make(map[string]manifestContract, len(manifest.Contracts))
	for _, contract := range manifest.Contracts {
		if contract.Kind != "module" {
			t.Fatalf("unexpected contract kind %q in plugin.contracts.json", contract.Kind)
		}
		if contract.Mode != "strict" {
			t.Fatalf("%s mode = %q, want strict", contract.Type, contract.Mode)
		}
		key := "module:" + contract.Type
		if _, exists := contracts[key]; exists {
			t.Fatalf("duplicate contract %q in plugin.contracts.json", key)
		}
		contracts[key] = contract.manifestContract
	}
	return contracts
}
