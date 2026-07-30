package otlp

import "testing"

func TestParseConfig_Tenant(t *testing.T) {
	t.Run("tenant sets the field", func(t *testing.T) {
		cfg, err := ParseConfig(map[string]interface{}{"endpoint": "otlp:4317", "tenant": "acme"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tenant != "acme" {
			t.Errorf("Tenant = %q, want acme", cfg.Tenant)
		}
	})

	t.Run("org_id alias", func(t *testing.T) {
		cfg, _ := ParseConfig(map[string]interface{}{"endpoint": "otlp:4317", "org_id": "team-b"})
		if cfg.Tenant != "team-b" {
			t.Errorf("Tenant = %q, want team-b (from org_id)", cfg.Tenant)
		}
	})

	t.Run("tenant wins over org_id", func(t *testing.T) {
		cfg, _ := ParseConfig(map[string]interface{}{"endpoint": "otlp:4317", "tenant": "a", "org_id": "b"})
		if cfg.Tenant != "a" {
			t.Errorf("Tenant = %q, want a (tenant wins)", cfg.Tenant)
		}
	})

	t.Run("control character rejected", func(t *testing.T) {
		if _, err := ParseConfig(map[string]interface{}{"endpoint": "otlp:4317", "tenant": "acme\r\nX-Injected: 1"}); err == nil {
			t.Error("a tenant with CRLF must be rejected (header injection)")
		}
	})
}

func TestWithTenantHeader(t *testing.T) {
	t.Run("adds X-Scope-OrgID when absent", func(t *testing.T) {
		got := withTenantHeader(map[string]string{"Authorization": "Bearer x"}, "acme")
		if got[xScopeOrgIDHeader] != "acme" {
			t.Errorf("%s = %q, want acme", xScopeOrgIDHeader, got[xScopeOrgIDHeader])
		}
		if got["Authorization"] != "Bearer x" {
			t.Error("existing header lost")
		}
	})

	t.Run("explicit header wins (case-insensitive)", func(t *testing.T) {
		got := withTenantHeader(map[string]string{"x-scope-orgid": "explicit"}, "acme")
		if got["x-scope-orgid"] != "explicit" {
			t.Errorf("explicit header overwritten: %v", got)
		}
		if _, dup := got[xScopeOrgIDHeader]; dup {
			t.Error("added a duplicate canonical-cased header alongside the explicit one")
		}
	})

	t.Run("empty tenant is a no-op returning the input", func(t *testing.T) {
		in := map[string]string{"a": "b"}
		if got := withTenantHeader(in, ""); len(got) != 1 || got["a"] != "b" {
			t.Errorf("empty tenant altered headers: %v", got)
		}
	})

	t.Run("does not mutate the input map", func(t *testing.T) {
		in := map[string]string{"Authorization": "Bearer x"}
		_ = withTenantHeader(in, "acme")
		if _, added := in[xScopeOrgIDHeader]; added {
			t.Error("input headers map was mutated")
		}
	})
}

// TestResolveTransport_InjectsTenant checks the tenant header lands on a signal
// even when that signal overrides headers (the injection is post-resolution).
func TestResolveTransport_InjectsTenant(t *testing.T) {
	cfg := Config{
		Endpoint: "otlp:4317",
		Tenant:   "acme",
		Headers:  map[string]string{"Authorization": "Bearer root"},
	}
	// Root headers path.
	rt := resolveTransport(cfg, SignalTransport{})
	if rt.headers[xScopeOrgIDHeader] != "acme" {
		t.Errorf("root path: %s = %q, want acme", xScopeOrgIDHeader, rt.headers[xScopeOrgIDHeader])
	}
	// Signal-override path (fully replaces root headers).
	rt2 := resolveTransport(cfg, SignalTransport{Headers: map[string]string{"Authorization": "Bearer sig"}})
	if rt2.headers[xScopeOrgIDHeader] != "acme" {
		t.Errorf("signal-override path: tenant header lost: %v", rt2.headers)
	}
	if rt2.headers["Authorization"] != "Bearer sig" {
		t.Errorf("signal header override lost: %v", rt2.headers)
	}
}
