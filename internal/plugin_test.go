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
	"gopkg.in/yaml.v3"
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

func TestPluginManifestMinEngineVersionMatchesDNSNamespaceRequirement(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var manifest struct {
		MinEngineVersion string `json:"minEngineVersion"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	if manifest.MinEngineVersion != "0.80.17" {
		t.Fatalf("minEngineVersion = %q, want 0.80.17 for plugin-owned dns command dispatch", manifest.MinEngineVersion)
	}
}

func TestPluginManifestDeclaresDNSCLICommand(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var manifest struct {
		Capabilities struct {
			CLICommands []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"cliCommands"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse plugin.json: %v", err)
	}
	for _, cmd := range manifest.Capabilities.CLICommands {
		if cmd.Name == "dns" && cmd.Description != "" {
			return
		}
	}
	t.Fatalf("plugin.json must declare a non-empty dns CLI command, got %+v", manifest.Capabilities.CLICommands)
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
		if contract.Kind == pb.ContractKind_CONTRACT_KIND_STEP {
			continue // step contracts validated in TestContractDeclaresStrictStepContracts
		}
		if contract.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE {
			continue // service method contracts (e.g. AdminContribution) tracked separately
		}
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
	// manifestContracts only contains module contracts (step contracts skipped in loadManifestContracts)
	if len(manifestContracts) != len(contractsByKey) {
		t.Fatalf("plugin.contracts.json module contract count = %d, runtime = %d", len(manifestContracts), len(contractsByKey))
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

// TestPlugin_StepTypes_EmptyPostPhase3b pins the Phase 3b decision:
// workflow-plugin-infra no longer registers any step types. The legacy
// infra.dns_record step was removed (peer-dispatch from a step-handler
// context is architecturally unsupported — cycle 3.5 I-NEW-1). Replaced
// the v1 tests that asserted infra.dns_record was present.
func TestPlugin_StepTypes_EmptyPostPhase3b(t *testing.T) {
	p := NewInfraPlugin()
	sp, ok := p.(sdk.StepProvider)
	if !ok {
		t.Fatal("expected StepProvider")
	}
	if got := sp.StepTypes(); len(got) != 0 {
		t.Errorf("StepTypes() = %v; want empty post-Phase-3b", got)
	}
	tsp, ok := p.(sdk.TypedStepProvider)
	if !ok {
		t.Fatal("expected TypedStepProvider")
	}
	if got := tsp.TypedStepTypes(); len(got) != 0 {
		t.Errorf("TypedStepTypes() = %v; want empty post-Phase-3b", got)
	}
}

// TestContractRegistry_HasNoStepContractsPostPhase3b mirrors the
// StepTypes assertion at the ContractRegistry level. The Phase 3b strip
// removed the infra.dns_record proto contract; ContractRegistry should
// now carry only module contracts.
func TestContractRegistry_HasNoStepContractsPostPhase3b(t *testing.T) {
	provider := NewInfraPlugin().(sdk.ContractProvider)
	registry := provider.ContractRegistry()
	for _, c := range registry.Contracts {
		if c.Kind == pb.ContractKind_CONTRACT_KIND_STEP {
			t.Errorf("ContractRegistry has unexpected step contract post-Phase-3b: %+v", c)
		}
	}
}

// TestPluginImplementsConfigProvider verifies that the plugin implements sdk.ConfigProvider
// and that ConfigFragment() succeeds and returns valid YAML. The ui_dist assets are embedded
// at compile time so this must work in all test environments.
func TestPluginImplementsConfigProvider(t *testing.T) {
	p := NewInfraPlugin()
	cp, ok := p.(sdk.ConfigProvider)
	if !ok {
		t.Fatal("plugin does not implement sdk.ConfigProvider")
	}
	fragment, err := cp.ConfigFragment()
	if err != nil {
		t.Fatalf("ConfigFragment returned unexpected error: %v", err)
	}
	if len(fragment) == 0 {
		t.Error("ConfigFragment returned empty byte slice")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(fragment, &parsed); err != nil {
		t.Errorf("ConfigFragment returned invalid YAML: %v", err)
	}
}

// TestConfigDataContainsStaticFileserver verifies the embedded config declares
// a static.fileserver module (required for serving the SPA).
func TestConfigDataContainsStaticFileserver(t *testing.T) {
	var cfg map[string]any
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	modules, ok := cfg["modules"].([]any)
	if !ok {
		t.Fatal("'modules' is not a list")
	}
	found := false
	for _, m := range modules {
		mod, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if modType, _ := mod["type"].(string); modType == "static.fileserver" {
			found = true
			break
		}
	}
	if !found {
		t.Error("embedded config.yaml does not declare a static.fileserver module")
	}
}

// TestInfraAdminModuleReturnsAdminContribution verifies the infra.admin module
// implements ServiceInvoker and returns the expected AdminContribution descriptor.
func TestInfraAdminModuleReturnsAdminContribution(t *testing.T) {
	mp := NewInfraPlugin().(sdk.ModuleProvider)
	module, err := mp.CreateModule("infra.admin", "infra-admin", map[string]any{
		"api_base_path": "/api/infra",
		"prefix":        "/admin/infra",
	})
	if err != nil {
		t.Fatalf("CreateModule(infra.admin): %v", err)
	}

	type serviceInvoker interface {
		InvokeMethod(method string, input map[string]any) (map[string]any, error)
	}
	invoker, ok := module.(serviceInvoker)
	if !ok {
		t.Fatalf("infra.admin module type %T must implement ServiceInvoker", module)
	}

	out, err := invoker.InvokeMethod("AdminContribution", nil)
	if err != nil {
		t.Fatalf("InvokeMethod(AdminContribution): %v", err)
	}

	enabled, _ := out["enabled"].(bool)
	if !enabled {
		t.Errorf("expected enabled=true, got %v", out["enabled"])
	}

	contribution, ok := out["contribution"].(map[string]any)
	if !ok {
		t.Fatalf("contribution = %T, want map[string]any", out["contribution"])
	}

	checks := map[string]string{
		"id":          "infra-resources",
		"title":       "Infrastructure",
		"category":    "operations",
		"path":        "/admin/infra",
		"render_mode": "iframe",
	}
	for field, want := range checks {
		if got, _ := contribution[field].(string); got != want {
			t.Errorf("contribution[%q] = %q, want %q", field, got, want)
		}
	}

	permissions, ok := contribution["permissions"].([]string)
	if !ok || len(permissions) == 0 {
		t.Errorf("permissions = %#v, want non-empty []string", contribution["permissions"])
	}

	// Verify infra.admin is in ModuleTypes.
	found := false
	for _, mt := range mp.(interface{ ModuleTypes() []string }).ModuleTypes() {
		if mt == "infra.admin" {
			found = true
			break
		}
	}
	if !found {
		t.Error("infra.admin not in plugin.ModuleTypes()")
	}
}

// TestInfraAdminModuleUnsupportedMethod verifies InvokeMethod returns an error
// for unknown method names.
func TestInfraAdminModuleUnsupportedMethod(t *testing.T) {
	mp := NewInfraPlugin().(sdk.ModuleProvider)
	module, err := mp.CreateModule("infra.admin", "infra-admin", nil)
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	type serviceInvoker interface {
		InvokeMethod(method string, input map[string]any) (map[string]any, error)
	}
	invoker := module.(serviceInvoker)
	if _, err := invoker.InvokeMethod("Unknown", nil); err == nil {
		t.Error("InvokeMethod(Unknown) should return error")
	}
}

// TestCreateTypedModule_InfraAdmin_AdminContribution exercises the typed-factory
// path for infra.admin: struct→map translation in CreateTypedModule and the
// InvokeMethod("AdminContribution") return value, including:
//   - fully-populated InfraAdminConfig (custom path, title, permissions)
//   - nil GetAdmin() (omitted sub-message) falls back to defaults
func TestCreateTypedModule_InfraAdmin_AdminContribution(t *testing.T) {
	provider := NewInfraPlugin().(sdk.TypedModuleProvider)

	type serviceInvoker interface {
		InvokeMethod(method string, input map[string]any) (map[string]any, error)
	}

	t.Run("fully_populated", func(t *testing.T) {
		cfg := &contracts.InfraAdminConfig{
			ApiBasePath: "/api/infra",
			Prefix:      "/admin/infra",
			Admin: &contracts.InfraAdminContributionConfig{
				Enabled:     true,
				Id:          "my-infra",
				Title:       "My Infra",
				Category:    "platform",
				Path:        "/admin/my-infra",
				RenderMode:  "iframe",
				AppContext:  "app",
				Permissions: []string{"infra:read", "infra:apply"},
			},
		}
		packed, err := anypb.New(cfg)
		if err != nil {
			t.Fatalf("anypb.New: %v", err)
		}
		mod, err := provider.CreateTypedModule("infra.admin", "test-admin", packed)
		if err != nil {
			t.Fatalf("CreateTypedModule: %v", err)
		}
		invoker, ok := mod.(serviceInvoker)
		if !ok {
			t.Fatalf("module type %T does not implement InvokeMethod", mod)
		}
		out, err := invoker.InvokeMethod("AdminContribution", nil)
		if err != nil {
			t.Fatalf("InvokeMethod: %v", err)
		}
		if got, _ := out["enabled"].(bool); !got {
			t.Errorf("enabled = %v, want true", out["enabled"])
		}
		contribution, ok := out["contribution"].(map[string]any)
		if !ok {
			t.Fatalf("contribution type = %T, want map[string]any", out["contribution"])
		}
		checks := map[string]string{
			"id":          "my-infra",
			"title":       "My Infra",
			"category":    "platform",
			"path":        "/admin/my-infra",
			"render_mode": "iframe",
		}
		for field, want := range checks {
			if got, _ := contribution[field].(string); got != want {
				t.Errorf("contribution[%q] = %q, want %q", field, got, want)
			}
		}
		perms := strSliceVal(contribution["permissions"])
		if len(perms) != 2 || perms[0] != "infra:read" {
			t.Errorf("permissions = %v, want [infra:read infra:apply]", perms)
		}
	})

	t.Run("nil_admin_uses_defaults", func(t *testing.T) {
		// InfraAdminConfig with no Admin sub-message: GetAdmin() returns nil.
		// CreateTypedModule must skip the admin map and let adminContributionFromConfig
		// fall back to its built-in defaults.
		cfg := &contracts.InfraAdminConfig{
			ApiBasePath: "/api/infra",
			Prefix:      "/admin/infra",
			// Admin intentionally omitted
		}
		packed, err := anypb.New(cfg)
		if err != nil {
			t.Fatalf("anypb.New: %v", err)
		}
		mod, err := provider.CreateTypedModule("infra.admin", "test-admin-defaults", packed)
		if err != nil {
			t.Fatalf("CreateTypedModule: %v", err)
		}
		invoker := mod.(serviceInvoker)
		out, err := invoker.InvokeMethod("AdminContribution", nil)
		if err != nil {
			t.Fatalf("InvokeMethod: %v", err)
		}
		if got, _ := out["enabled"].(bool); !got {
			t.Errorf("enabled = %v, want true (default)", out["enabled"])
		}
		contribution, ok := out["contribution"].(map[string]any)
		if !ok {
			t.Fatalf("contribution type = %T, want map[string]any", out["contribution"])
		}
		// Defaults from adminContributionFromConfig
		defaults := map[string]string{
			"id":          "infra-resources",
			"title":       "Infrastructure",
			"category":    "operations",
			"path":        "/admin/infra", // falls back to Prefix
			"render_mode": "iframe",
		}
		for field, want := range defaults {
			if got, _ := contribution[field].(string); got != want {
				t.Errorf("default contribution[%q] = %q, want %q", field, got, want)
			}
		}
		perms := strSliceVal(contribution["permissions"])
		if len(perms) == 0 {
			t.Error("default permissions should be non-empty")
		}
	})
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
		if contract.Kind == "step" {
			continue // skip; step contracts loaded separately
		}
		if contract.Kind == "service_method" {
			continue // skip; service method contracts tracked separately
		}
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
