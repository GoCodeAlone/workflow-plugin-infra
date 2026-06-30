// Package internal contains the secret-admin pipeline steps.
//
// step.secret_list / step.secret_delete / step.secret_vault_status /
// step.secret_vault_test are the first stepTypes owned by workflow-plugin-infra.
// They are written NATIVELY as in-process plugin.StepFactory (ADR 0056
// dual-shape): Execute takes a *interfaces.PipelineContext and returns an
// *interfaces.StepResult directly — no reverse sdk bridge is required (unlike
// workflow-plugin-auth's existing sdk steps).
//
// Each step resolves a secrets.Provider from the host application's service
// registry by the configured `module:` key. Built-in secrets modules
// (module/secrets_vault.go, module/secrets_aws.go) register themselves under
// their config `name` and expose the underlying provider via a Provider()
// accessor; this file mirrors the resolver-accessor pattern established by
// the engine's step.secret_set (module/pipeline_step_secret_set.go).
package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// Exported constructors for the four secret-admin step factories. These wrap
// the package-private factory builders so the root-package EnginePlugin entry
// (engine_plugin.go) can register them as in-process plugin.StepFactory values
// without exposing the step structs themselves.

// NewSecretListStepFactory returns a factory building step.secret_list steps.
func NewSecretListStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return newSecretListStepFactory()
}

// NewSecretDeleteStepFactory returns a factory building step.secret_delete steps.
func NewSecretDeleteStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return newSecretDeleteStepFactory()
}

// NewSecretVaultStatusStepFactory returns a factory building step.secret_vault_status steps.
func NewSecretVaultStatusStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return newSecretVaultStatusStepFactory()
}

// NewSecretVaultTestStepFactory returns a factory building step.secret_vault_test steps.
func NewSecretVaultTestStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return newSecretVaultTestStepFactory()
}

// providerAccessor is implemented by secrets module wrappers that register
// themselves under their config name but expose the underlying secrets.Provider
// via a Provider() accessor (module.SecretsVaultModule.Provider at
// module/secrets_vault.go:136; module.SecretsAWSModule.Provider likewise).
// We re-declare the minimal local interface rather than depending on the
// engine's module package to avoid the engine's heavy import surface.
type providerAccessor interface {
	Provider() secrets.Provider
}

// vaultTestProbeKey is a benign sentinel key used by step.secret_vault_test to
// probe backend connectivity. A not-found response is healthy (it proves the
// backend answers); any other error is a connectivity failure. The key is
// intentionally namespaced to never collide with real secrets.
const vaultTestProbeKey = "__vault_test__"

// resolveProvider looks up the secrets.Provider backing the named module from
// the application service registry. The service entry may be the module wrapper
// (which exposes Provider()) or the provider itself.
func resolveProvider(app modular.Application, stepName, moduleKey string) (secrets.Provider, error) {
	if app == nil {
		return nil, fmt.Errorf("secret-admin step %q: no application context (in-process entry misconfigured)", stepName)
	}
	svc, ok := app.SvcRegistry()[moduleKey]
	if !ok {
		return nil, fmt.Errorf("secret-admin step %q: secrets module %q not found in service registry", stepName, moduleKey)
	}
	// Direct: the registered service itself implements secrets.Provider.
	if provider, ok := svc.(secrets.Provider); ok {
		return provider, nil
	}
	// Indirect: the registered service is the module wrapper, which exposes
	// its underlying secrets.Provider via Provider() (the secrets.vault /
	// secrets.aws module pattern).
	if accessor, ok := svc.(providerAccessor); ok {
		underlying := accessor.Provider()
		if underlying == nil {
			return nil, fmt.Errorf("secret-admin step %q: module %q Provider() returned nil (module not started?)", stepName, moduleKey)
		}
		return underlying, nil
	}
	return nil, fmt.Errorf("secret-admin step %q: service %q is neither a secrets.Provider nor exposes Provider()", stepName, moduleKey)
}

// ─── step.secret_list ──────────────────────────────────────────────────────

// secretListStep lists all secret keys held by a named secrets module.
//
//	config:
//	  module: zoom-secrets
type secretListStep struct {
	name      string
	moduleKey string
	app       modular.Application
}

func newSecretListStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
		moduleKey, _ := config["module"].(string)
		if moduleKey == "" {
			return nil, fmt.Errorf("secret_list step %q: 'module' is required", name)
		}
		return &secretListStep{name: name, moduleKey: moduleKey, app: app}, nil
	}
}

func (s *secretListStep) Name() string { return s.name }

func (s *secretListStep) Execute(ctx context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	provider, err := resolveProvider(s.app, s.name, s.moduleKey)
	if err != nil {
		return nil, err
	}
	keys, err := provider.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("secret_list step %q: List failed: %w", s.name, err)
	}
	return &interfaces.StepResult{Output: map[string]any{"keys": keys}}, nil
}

// ─── step.secret_delete ────────────────────────────────────────────────────

// secretDeleteStep removes a single secret by key from a named secrets module
// and confirms the deletion in its output.
//
//	config:
//	  module: zoom-secrets
//	  key: client_secret
type secretDeleteStep struct {
	name      string
	moduleKey string
	key       string
	app       modular.Application
}

func newSecretDeleteStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
		moduleKey, _ := config["module"].(string)
		if moduleKey == "" {
			return nil, fmt.Errorf("secret_delete step %q: 'module' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("secret_delete step %q: 'key' is required", name)
		}
		return &secretDeleteStep{name: name, moduleKey: moduleKey, key: key, app: app}, nil
	}
}

func (s *secretDeleteStep) Name() string { return s.name }

func (s *secretDeleteStep) Execute(ctx context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	provider, err := resolveProvider(s.app, s.name, s.moduleKey)
	if err != nil {
		return nil, err
	}
	if err := provider.Delete(ctx, s.key); err != nil {
		return nil, fmt.Errorf("secret_delete step %q: Delete(%q) failed: %w", s.name, s.key, err)
	}
	return &interfaces.StepResult{Output: map[string]any{"deleted": s.key}}, nil
}

// ─── step.secret_vault_status (read-only) ──────────────────────────────────

// secretVaultStatusStep reports backend identity + key presence. It is
// READ-ONLY and MUST NOT mutate the store. When the provider implements
// secrets.MetadataProvider, key_count reflects StatAll; otherwise key_count
// falls back to List. Health is "ok" when the probe succeeds.
//
//	config:
//	  module: zoom-secrets
type secretVaultStatusStep struct {
	name      string
	moduleKey string
	app       modular.Application
}

func newSecretVaultStatusStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
		moduleKey, _ := config["module"].(string)
		if moduleKey == "" {
			return nil, fmt.Errorf("secret_vault_status step %q: 'module' is required", name)
		}
		return &secretVaultStatusStep{name: name, moduleKey: moduleKey, app: app}, nil
	}
}

func (s *secretVaultStatusStep) Name() string { return s.name }

func (s *secretVaultStatusStep) Execute(ctx context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	provider, err := resolveProvider(s.app, s.name, s.moduleKey)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"backend": provider.Name(),
		"health":  "ok",
	}
	// Prefer MetadataProvider.StatAll when available (presence + freshness);
	// fall back to List. Both are read-only.
	var keyCount int
	if meta, ok := provider.(secrets.MetadataProvider); ok {
		metas, err := meta.StatAll(ctx)
		if err != nil {
			out["health"] = "degraded"
			out["error"] = err.Error()
		} else {
			keyCount = len(metas)
		}
	} else {
		keys, err := provider.List(ctx)
		if err != nil {
			out["health"] = "degraded"
			out["error"] = err.Error()
		} else {
			keyCount = len(keys)
		}
	}
	out["key_count"] = keyCount
	return &interfaces.StepResult{Output: out}, nil
}

// ─── step.secret_vault_test (read-only connectivity probe) ─────────────────

// secretVaultTestStep performs a benign connectivity probe against the secrets
// backend. It issues a Get for a sentinel key that is expected to be absent; a
// not-found response proves the backend answers, while any other error is
// surfaced as ok=false in the step output. It does NOT mutate the store and
// does NOT attempt runtime reconfiguration (the secrets.vault module is
// Start-time-only).
//
//	config:
//	  module: zoom-secrets
type secretVaultTestStep struct {
	name      string
	moduleKey string
	app       modular.Application
}

func newSecretVaultTestStepFactory() func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
	return func(name string, config map[string]any, app modular.Application) (interfaces.PipelineStep, error) {
		moduleKey, _ := config["module"].(string)
		if moduleKey == "" {
			return nil, fmt.Errorf("secret_vault_test step %q: 'module' is required", name)
		}
		return &secretVaultTestStep{name: name, moduleKey: moduleKey, app: app}, nil
	}
}

func (s *secretVaultTestStep) Name() string { return s.name }

func (s *secretVaultTestStep) Execute(ctx context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	provider, err := resolveProvider(s.app, s.name, s.moduleKey)
	if err != nil {
		return nil, err
	}
	// Prefer AccessChecker.CheckAccess when the provider implements it; this is
	// the canonical "is the backend usable" probe and avoids leaking probe
	// semantics into Get. Fall back to a benign Get of a sentinel key.
	if checker, ok := provider.(secrets.AccessChecker); ok {
		if err := checker.CheckAccess(ctx); err != nil {
			// A hard connectivity failure is surfaced, not raised: this step is
			// a read-only probe and must not abort the pipeline.
			return &interfaces.StepResult{Output: map[string]any{
				"ok":    false,
				"error": err.Error(),
			}}, nil
		}
		return &interfaces.StepResult{Output: map[string]any{"ok": true}}, nil
	}
	if _, err := provider.Get(ctx, vaultTestProbeKey); err != nil {
		// not-found is the healthy case: the backend answered.
		if !errors.Is(err, secrets.ErrNotFound) {
			return &interfaces.StepResult{Output: map[string]any{
				"ok":    false,
				"error": err.Error(),
			}}, nil
		}
	}
	return &interfaces.StepResult{Output: map[string]any{"ok": true}}, nil
}
