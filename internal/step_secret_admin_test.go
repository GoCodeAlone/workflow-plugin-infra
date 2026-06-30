package internal

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// fakeVaultProvider is a minimal secrets.Provider + secrets.MetadataProvider +
// secrets.AccessChecker used to exercise the secret-admin steps without a real
// Vault subprocess. It is the test double for module.SecretsVaultModule's
// Provider() accessor.
type fakeVaultProvider struct {
	name      string
	store     map[string]string
	accessErr error // optional: injected hard error for connectivity probe
}

func newFakeVaultProvider(name string) *fakeVaultProvider {
	return &fakeVaultProvider{name: name, store: map[string]string{}}
}

func (p *fakeVaultProvider) Name() string { return p.name }
func (p *fakeVaultProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := p.store[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *fakeVaultProvider) Set(_ context.Context, key, value string) error {
	p.store[key] = value
	return nil
}
func (p *fakeVaultProvider) Delete(_ context.Context, key string) error {
	if _, ok := p.store[key]; !ok {
		return secrets.ErrNotFound
	}
	delete(p.store, key)
	return nil
}
func (p *fakeVaultProvider) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(p.store))
	for k := range p.store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// StatAll implements secrets.MetadataProvider.
func (p *fakeVaultProvider) StatAll(_ context.Context) ([]secrets.SecretMeta, error) {
	keys, _ := p.List(context.Background())
	metas := make([]secrets.SecretMeta, 0, len(keys))
	for _, k := range keys {
		metas = append(metas, secrets.SecretMeta{Name: k, Exists: true})
	}
	return metas, nil
}

// CheckAccess implements secrets.AccessChecker.
func (p *fakeVaultProvider) CheckAccess(_ context.Context) error {
	return p.accessErr
}

// vaultModuleStub mirrors module.SecretsVaultModule's ProvidesServices contract:
// it registers itself under its config name and exposes a Provider() accessor
// returning the underlying secrets.Provider (module/secrets_vault.go:44,136).
type vaultModuleStub struct {
	name string
	p    *fakeVaultProvider
}

func (m *vaultModuleStub) Name() string                                { return m.name }
func (m *vaultModuleStub) Provider() secrets.Provider                  { return m.p }
func (m *vaultModuleStub) Get(ctx context.Context, k string) (string, error) {
	return m.p.Get(ctx, k)
}

// stubApp is a minimal modular.Application whose only meaningful method is
// SvcRegistry(); the rest are no-ops sufficient to satisfy the interface for
// secret-admin step tests. It mirrors module.MockApplication's no-op pattern
// (engine module/trigger_test_helpers.go) without the engine dependency.
type stubApp struct {
	svcs modular.ServiceRegistry
}

func (a *stubApp) SvcRegistry() modular.ServiceRegistry              { return a.svcs }
func (a *stubApp) RegisterService(_ string, _ any) error             { return nil }
func (a *stubApp) GetService(name string, _ any) error {
	return fmt.Errorf("stubApp: service %q not registered", name)
}
func (a *stubApp) ConfigProvider() modular.ConfigProvider            { return nil }
func (a *stubApp) ConfigSections() map[string]modular.ConfigProvider { return nil }
func (a *stubApp) GetConfigSection(_ string) (modular.ConfigProvider, error) {
	return nil, fmt.Errorf("stubApp: no config sections")
}
func (a *stubApp) RegisterConfigSection(_ string, _ modular.ConfigProvider) {}
func (a *stubApp) RegisterModule(_ modular.Module)                         {}
func (a *stubApp) GetModule(_ string) modular.Module                       { return nil }
func (a *stubApp) GetAllModules() map[string]modular.Module                { return nil }
func (a *stubApp) GetServicesByModule(_ string) []string                   { return nil }
func (a *stubApp) GetServiceEntry(_ string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}
func (a *stubApp) GetServicesByInterface(_ reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}
func (a *stubApp) Logger() modular.Logger           { return nil }
func (a *stubApp) SetLogger(_ modular.Logger)       {}
func (a *stubApp) Init() error                      { return nil }
func (a *stubApp) Start() error                     { return nil }
func (a *stubApp) Stop() error                      { return nil }
func (a *stubApp) Run() error                       { return nil }
func (a *stubApp) IsVerboseConfig() bool            { return false }
func (a *stubApp) SetVerboseConfig(_ bool)          {}
func (a *stubApp) StartTime() time.Time             { return time.Time{} }
func (a *stubApp) OnConfigLoaded(_ func(modular.Application) error) {}

// registerFakeVault installs a vaultModuleStub under moduleKey in a stubApp
// seeded with the given key/values.
func registerFakeVault(moduleKey string, seed map[string]string) (*stubApp, *fakeVaultProvider) {
	p := newFakeVaultProvider("fake-vault")
	for k, v := range seed {
		p.store[k] = v
	}
	app := &stubApp{svcs: modular.ServiceRegistry{moduleKey: &vaultModuleStub{name: moduleKey, p: p}}}
	return app, p
}

// --- step.secret_list ---

func TestSecretListStep_ReturnsKeys(t *testing.T) {
	app, _ := registerFakeVault("zoom-secrets", map[string]string{
		"client_id":     "abc",
		"client_secret": "shh",
	})
	factory := newSecretListStepFactory()
	step, err := factory("list-secrets", map[string]any{"module": "zoom-secrets"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	res, err := step.Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	keys, _ := res.Output["keys"].([]string)
	want := []string{"client_id", "client_secret"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
}

func TestSecretListStep_MissingModuleCfg(t *testing.T) {
	factory := newSecretListStepFactory()
	_, err := factory("list-secrets", map[string]any{}, &stubApp{})
	if err == nil {
		t.Fatal("expected error for missing 'module' config")
	}
}

func TestSecretListStep_ModuleNotRegistered(t *testing.T) {
	// Provider resolution is deferred to Execute (matches the engine's
	// step.secret_set pattern); a missing module surfaces at run time, not at
	// factory construction.
	factory := newSecretListStepFactory()
	step, err := factory("list-secrets", map[string]any{"module": "nope"}, &stubApp{})
	if err != nil {
		t.Fatalf("factory error (expected at Execute, not construction): %v", err)
	}
	_, err = step.Execute(context.Background(), newCtx())
	if err == nil {
		t.Fatal("expected Execute error for unregistered module")
	}
}

// --- step.secret_delete ---

func TestSecretDeleteStep_DeletesAndConfirms(t *testing.T) {
	app, p := registerFakeVault("zoom-secrets", map[string]string{"client_id": "abc", "client_secret": "shh"})
	factory := newSecretDeleteStepFactory()
	step, err := factory("del-secret", map[string]any{"module": "zoom-secrets", "key": "client_id"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	res, err := step.Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := res.Output["deleted"]; got != "client_id" {
		t.Fatalf("deleted = %v, want client_id", got)
	}
	if _, exists := p.store["client_id"]; exists {
		t.Fatal("client_id still present after delete")
	}
	if _, exists := p.store["client_secret"]; !exists {
		t.Fatal("client_secret should remain after deleting client_id")
	}
}

func TestSecretDeleteStep_MissingKeyCfg(t *testing.T) {
	app, _ := registerFakeVault("zoom-secrets", nil)
	factory := newSecretDeleteStepFactory()
	_, err := factory("del-secret", map[string]any{"module": "zoom-secrets"}, app)
	if err == nil {
		t.Fatal("expected error for missing 'key' config")
	}
}

// --- step.secret_vault_status ---

func TestSecretVaultStatusStep_NameAndCount(t *testing.T) {
	app, _ := registerFakeVault("zoom-secrets", map[string]string{"a": "1", "b": "2"})
	factory := newSecretVaultStatusStepFactory()
	step, err := factory("vault-status", map[string]any{"module": "zoom-secrets"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	res, err := step.Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := res.Output["backend"]; got != "fake-vault" {
		t.Fatalf("backend = %v, want fake-vault", got)
	}
	count, _ := res.Output["key_count"].(int)
	if count != 2 {
		t.Fatalf("key_count = %d, want 2", count)
	}
}

// --- step.secret_vault_test (connectivity probe) ---

func TestSecretVaultTestStep_Healthy(t *testing.T) {
	app, p := registerFakeVault("zoom-secrets", nil)
	p.accessErr = nil
	factory := newSecretVaultTestStepFactory()
	step, err := factory("vault-test", map[string]any{"module": "zoom-secrets"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	res, err := step.Execute(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if ok, _ := res.Output["ok"].(bool); !ok {
		t.Fatalf("ok = false, want true; diagnostics=%v", res.Output["error"])
	}
}

func TestSecretVaultTestStep_HardErrorFlagged(t *testing.T) {
	app, p := registerFakeVault("zoom-secrets", nil)
	p.accessErr = errors.New("vault unreachable")
	factory := newSecretVaultTestStepFactory()
	step, err := factory("vault-test", map[string]any{"module": "zoom-secrets"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	res, err := step.Execute(context.Background(), newCtx())
	// A hard connectivity error must NOT abort the pipeline; it is surfaced
	// as ok=false in the step output (read-only probe).
	if err != nil {
		t.Fatalf("Execute returned error for probe (should be in output): %v", err)
	}
	if ok, _ := res.Output["ok"].(bool); ok {
		t.Fatal("ok = true, want false on hard error")
	}
	if msg, _ := res.Output["error"].(string); msg == "" {
		t.Fatal("error diagnostic missing from output")
	}
}

// newCtx returns a minimal non-nil PipelineContext for step execution.
func newCtx() *interfaces.PipelineContext {
	return &interfaces.PipelineContext{
		TriggerData: map[string]any{},
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
		Metadata:    map[string]any{},
	}
}
