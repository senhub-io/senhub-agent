package configuration

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// TestSubstitute_SecretScheme verifies the ${secret:} scheme resolves through
// the active provider, honours defaults, and fails loudly when a referenced
// secret is missing with no default.
func TestSubstitute_SecretScheme(t *testing.T) {
	mp := secret.NewMemoryProvider()
	_ = mp.Set("veeam-prod.password", secret.New("h3llo$ecret"))
	secret.SetProvider(mp)
	t.Cleanup(func() { secret.SetProvider(nil) })

	data := map[string]interface{}{
		"password": "${secret:veeam-prod.password}",
		"fallback": "${secret:absent:-default-value}",
		"mixed":    "Bearer ${secret:veeam-prod.password}",
	}
	if err := Substitute(&data); err != nil {
		t.Fatalf("Substitute: %v", err)
	}
	if got := data["password"]; got != "h3llo$ecret" {
		t.Errorf("password resolved to %q", got)
	}
	if got := data["fallback"]; got != "default-value" {
		t.Errorf("fallback resolved to %q", got)
	}
	if got := data["mixed"]; got != "Bearer h3llo$ecret" {
		t.Errorf("mixed resolved to %q", got)
	}

	// Missing secret with no default → error (never silently empty).
	missing := map[string]interface{}{"x": "${secret:does-not-exist}"}
	err := Substitute(&missing)
	if err == nil {
		t.Fatal("missing secret without default: expected error")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the secret: %v", err)
	}
}

// TestSanitizeParamsForLog_Recursive verifies nested credentials are masked.
func TestSanitizeParamsForLog_Recursive(t *testing.T) {
	in := map[string]interface{}{
		"username": "svc",
		"password": "top",
		"director": map[string]interface{}{
			"auth": map[string]interface{}{"password": "nested-pw", "user": "admin"},
		},
		"nodes": []interface{}{ // neutral parent key → recursion descends per element
			map[string]interface{}{"auth_password": "u0", "name": "n0"},
		},
	}
	out := SanitizeParamsForLog(in)

	// Original is never mutated.
	if in["password"] != "top" {
		t.Error("original map was mutated")
	}
	if out["password"] != "***" {
		t.Errorf("flat password not masked: %v", out["password"])
	}
	// `auth` is a secret name under the shared rule: the whole block is
	// masked, the nested password and user with it.
	director := out["director"].(map[string]interface{})
	if director["auth"] != "***" {
		t.Errorf("auth block not masked: %v", director["auth"])
	}
	nodes := out["nodes"].([]interface{})
	u0 := nodes[0].(map[string]interface{})
	if u0["auth_password"] != "***" {
		t.Errorf("indexed auth_password not masked: %v", u0["auth_password"])
	}
	if u0["name"] != "n0" {
		t.Errorf("non-secret 'name' should survive: %v", u0["name"])
	}
}
