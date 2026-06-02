package internal

import (
	"context"
	"fmt"
)

// infraAdminModule implements the infra.admin module type, which contributes
// the infrastructure management SPA to the workflow admin panel.
type infraAdminModule struct {
	name   string
	config map[string]any
}

func newInfraAdminModule(name string, config map[string]any) *infraAdminModule {
	c := make(map[string]any, len(config))
	for k, v := range config {
		c[k] = v
	}
	return &infraAdminModule{name: name, config: c}
}

func (m *infraAdminModule) Init() error { return nil }

func (m *infraAdminModule) Start(_ context.Context) error { return nil }

func (m *infraAdminModule) Stop(_ context.Context) error { return nil }

// InvokeMethod implements sdk.ServiceInvoker for the infra.admin module type.
// The only supported method is "AdminContribution", which returns the admin panel
// nav contribution descriptor for the infra SPA.
func (m *infraAdminModule) InvokeMethod(method string, _ map[string]any) (map[string]any, error) {
	switch method {
	case "AdminContribution":
		contribution := adminContributionFromConfig(m.config)
		return map[string]any{
			"enabled":      contribution["enabled"],
			"contribution": contribution,
		}, nil
	default:
		return nil, fmt.Errorf("infra.admin method %q is not supported", method)
	}
}

// adminContributionFromConfig builds the AdminContribution descriptor from the
// module's config map, applying defaults that match the infra SPA's route.
func adminContributionFromConfig(config map[string]any) map[string]any {
	admin := mapValue(config["admin"])

	path := defaultStr(strVal(admin["path"]), defaultStr(strVal(config["prefix"]), "/admin/infra"))
	appContext := defaultStr(strVal(admin["app_context"]), defaultStr(strVal(config["app_context"]), "app"))

	permissions := strSliceVal(admin["permissions"])
	if len(permissions) == 0 {
		permissions = []string{
			"infra:resource:read",
			"infra:plan",
			"infra:apply",
			"infra:secret:write",
			"infra:exec:direct",
		}
	}

	enabled := true
	if raw, ok := admin["enabled"].(bool); ok {
		enabled = raw
	}

	return map[string]any{
		"enabled":     enabled,
		"module":      defaultStr(strVal(admin["module"]), "admin"),
		"id":          defaultStr(strVal(admin["id"]), "infra-resources"),
		"title":       defaultStr(strVal(admin["title"]), "Infrastructure"),
		"category":    defaultStr(strVal(admin["category"]), "operations"),
		"path":        path,
		"render_mode": defaultStr(strVal(admin["render_mode"]), "iframe"),
		"app_context": appContext,
		"permissions": permissions,
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func defaultStr(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

func mapValue(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for key, item := range m {
			if s, ok := key.(string); ok {
				out[s] = item
			}
		}
		return out
	default:
		return map[string]any{}
	}
}

func strSliceVal(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
