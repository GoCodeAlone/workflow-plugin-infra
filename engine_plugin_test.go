package workflowplugininfra

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/secrets"
)

// TestInfraEnginePlugin_Shape verifies the in-process EnginePlugin entry
// (ADR 0056 dual-shape) exposes the expected manifest name + the four
// secret-admin step factories, WITHOUT touching the gRPC/sdk path.
func TestInfraEnginePlugin_Shape(t *testing.T) {
	p := NewInfraEnginePlugin()
	if p == nil {
		t.Fatal("NewInfraEnginePlugin() returned nil")
	}
	var _ plugin.EnginePlugin = p

	man := p.EngineManifest()
	if man == nil {
		t.Fatal("EngineManifest() returned nil")
	}
	if man.Name != "workflow-plugin-infra" {
		t.Fatalf("EngineManifest().Name = %q, want %q", man.Name, "workflow-plugin-infra")
	}

	factories := p.StepFactories()
	if factories == nil {
		t.Fatal("StepFactories() returned nil")
	}
	want := []string{
		"step.secret_list",
		"step.secret_delete",
		"step.secret_vault_status",
		"step.secret_vault_test",
	}
	got := make([]string, 0, len(factories))
	for k := range factories {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StepFactories() keys = %v, want %v", got, want)
	}
}

// TestInfraEnginePlugin_NativeStepsExecute verifies the four factories return
// in-process steps (interfaces.PipelineStep) that execute against a
// modular.Application exposing a fake secrets Provider. This confirms the
// steps are NATIVE in-process (no reverse sdk bridge): they take a
// *interfaces.PipelineContext directly.
func TestInfraEnginePlugin_NativeStepsExecute(t *testing.T) {
	p := NewInfraEnginePlugin()

	// Build a host app exposing a fake secrets vault under "test-vault".
	provider := &fakeProvider{name: "fake-vault", store: map[string]string{"a": "1", "b": "2"}}
	app := &miniApp{svcs: modular.ServiceRegistry{
		"test-vault": &vaultStub{name: "test-vault", p: provider},
	}}

	// secret_list
	listF := p.StepFactories()["step.secret_list"]
	listStep, err := listF("list", map[string]any{"module": "test-vault"}, app)
	if err != nil {
		t.Fatalf("secret_list factory: %v", err)
	}
	listRes, err := asPipelineStep(listStep).Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("secret_list Execute: %v", err)
	}
	keys, _ := listRes.Output["keys"].([]string)
	if len(keys) != 2 {
		t.Fatalf("secret_list keys = %v, want 2", keys)
	}

	// secret_delete
	delF := p.StepFactories()["step.secret_delete"]
	delStep, err := delF("del", map[string]any{"module": "test-vault", "key": "a"}, app)
	if err != nil {
		t.Fatalf("secret_delete factory: %v", err)
	}
	delRes, err := asPipelineStep(delStep).Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("secret_delete Execute: %v", err)
	}
	if delRes.Output["deleted"] != "a" {
		t.Fatalf("secret_delete deleted = %v, want a", delRes.Output["deleted"])
	}

	// secret_vault_status
	statusF := p.StepFactories()["step.secret_vault_status"]
	statusStep, err := statusF("status", map[string]any{"module": "test-vault"}, app)
	if err != nil {
		t.Fatalf("secret_vault_status factory: %v", err)
	}
	statusRes, err := asPipelineStep(statusStep).Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("secret_vault_status Execute: %v", err)
	}
	if statusRes.Output["backend"] != "fake-vault" {
		t.Fatalf("backend = %v, want fake-vault", statusRes.Output["backend"])
	}

	// secret_vault_test
	testF := p.StepFactories()["step.secret_vault_test"]
	testStep, err := testF("test", map[string]any{"module": "test-vault"}, app)
	if err != nil {
		t.Fatalf("secret_vault_test factory: %v", err)
	}
	testRes, err := asPipelineStep(testStep).Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("secret_vault_test Execute: %v", err)
	}
	if ok, _ := testRes.Output["ok"].(bool); !ok {
		t.Fatalf("secret_vault_test ok = false, want true; err=%v", testRes.Output["error"])
	}
}

// asPipelineStep asserts the factory's `any` return is an in-process
// interfaces.PipelineStep — confirming the steps are native (not reverse-bridged
// sdk steps).
func asPipelineStep(v any) interfaces.PipelineStep {
	s, ok := v.(interfaces.PipelineStep)
	if !ok {
		panic("step does not implement interfaces.PipelineStep (not native in-process)")
	}
	return s
}

func newCtx() *interfaces.PipelineContext {
	return &interfaces.PipelineContext{
		TriggerData: map[string]any{},
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		Metadata:    map[string]any{},
	}
}

// --- minimal test doubles (root-package versions; the internal package has
// its own equivalents for its unit tests) ---

type fakeProvider struct {
	name  string
	store map[string]string
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Get(_ context.Context, k string) (string, error) {
	if v, ok := p.store[k]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (p *fakeProvider) Set(_ context.Context, k, v string) error { p.store[k] = v; return nil }
func (p *fakeProvider) Delete(_ context.Context, k string) error {
	if _, ok := p.store[k]; !ok {
		return secrets.ErrNotFound
	}
	delete(p.store, k)
	return nil
}
func (p *fakeProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.store))
	for k := range p.store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

type vaultStub struct {
	name string
	p    *fakeProvider
}

func (m *vaultStub) Name() string                   { return m.name }
func (m *vaultStub) Provider() secrets.Provider     { return m.p }

// miniApp is a minimal modular.Application for the root-package test. It only
// meaningfully implements SvcRegistry(); the rest are no-ops.
type miniApp struct {
	svcs modular.ServiceRegistry
}

func (a *miniApp) SvcRegistry() modular.ServiceRegistry { return a.svcs }

// The remaining modular.Application methods are no-ops; only SvcRegistry() is
// exercised by the secret-admin steps (mirrors internal.stubApp).
func (a *miniApp) RegisterService(_ string, _ any) error             { return nil }
func (a *miniApp) GetService(name string, _ any) error {
	return fmt.Errorf("miniApp: service %q not registered", name)
}
func (a *miniApp) ConfigProvider() modular.ConfigProvider            { return nil }
func (a *miniApp) ConfigSections() map[string]modular.ConfigProvider { return nil }
func (a *miniApp) GetConfigSection(_ string) (modular.ConfigProvider, error) {
	return nil, fmt.Errorf("miniApp: no config sections")
}
func (a *miniApp) RegisterConfigSection(_ string, _ modular.ConfigProvider) {}
func (a *miniApp) RegisterModule(_ modular.Module)                         {}
func (a *miniApp) GetModule(_ string) modular.Module                       { return nil }
func (a *miniApp) GetAllModules() map[string]modular.Module                { return nil }
func (a *miniApp) GetServicesByModule(_ string) []string                   { return nil }
func (a *miniApp) GetServiceEntry(_ string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}
func (a *miniApp) GetServicesByInterface(_ reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}
func (a *miniApp) Logger() modular.Logger           { return nil }
func (a *miniApp) SetLogger(_ modular.Logger)       {}
func (a *miniApp) Init() error                      { return nil }
func (a *miniApp) Start() error                     { return nil }
func (a *miniApp) Stop() error                      { return nil }
func (a *miniApp) Run() error                       { return nil }
func (a *miniApp) IsVerboseConfig() bool            { return false }
func (a *miniApp) SetVerboseConfig(_ bool)          {}
func (a *miniApp) StartTime() time.Time             { return time.Time{} }
func (a *miniApp) OnConfigLoaded(_ func(modular.Application) error) {}
