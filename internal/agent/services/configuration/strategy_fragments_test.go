package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrategyFragments_CreateUpdateDisableDelete(t *testing.T) {
	main := multiFileForFragments(t)
	dir := filepath.Join(filepath.Dir(main), "strategies.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "00-http.yaml"), []byte("# install\nhttp:\n  port: 8080\n  endpoints: [\"web\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := CreateStrategyFragment(main, "otlp", map[string]interface{}{
		"endpoint": "collector:4317",
		"headers":  map[string]interface{}{"Authorization": "Bearer tok", "X-Tenant": "acme"},
	}, true, []string{"headers"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), managedFragmentHeader) || strings.Contains(string(raw), "tok") || strings.Contains(string(raw), "acme") || !strings.Contains(string(raw), "${secret:strategies.otlp.headers.Authorization}") {
		t.Errorf("fragment must be managed with every header value sealed, got:\n%s", raw)
	}
	if _, err := CreateStrategyFragment(main, "otlp", map[string]interface{}{"endpoint": "x"}, true, nil); err == nil {
		t.Error("a second otlp must be refused")
	}
	if _, err := CreateStrategyFragment(main, "Bad Name", nil, true, nil); err == nil {
		t.Error("an invalid name must be refused")
	}
	if p, err := CreateStrategyFragment(main, "prtg", map[string]interface{}{"server_url": "http://p"}, false, nil); err != nil || !strings.HasSuffix(p, ".disabled") {
		t.Errorf("a disabled create writes the .disabled file directly, got %s %v", p, err)
	}
	if _, err := CreateStrategyFragment(main, "prtg", map[string]interface{}{"server_url": "http://p"}, true, nil); err == nil {
		t.Error("a disabled output is still an existing one")
	}
	if err := os.WriteFile(filepath.Join(dir, "20-broken.yaml"), []byte("otlp: [\nprtg: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	frags, err := ListStrategyFragments(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 4 {
		t.Fatalf("want http, otlp, the disabled prtg and the broken file, got %+v", frags)
	}
	for _, f := range frags {
		switch f.Name {
		case "20-broken":
			if f.Error == "" || f.Params != nil {
				t.Errorf("an unreadable file is listed with its error: %+v", f)
			}
		case "http":
			if f.Managed || !f.Enabled || f.Params["port"] != 8080 {
				t.Errorf("http fragment read wrong: %+v", f)
			}
		case "otlp":
			if !f.Managed || !f.Enabled {
				t.Errorf("otlp fragment read wrong: %+v", f)
			}
		}
	}

	// The broken file has served its purpose; the loader itself still refuses it.
	if err := os.Remove(filepath.Join(dir, "20-broken.yaml")); err != nil {
		t.Fatal(err)
	}
	disabled, err := UpdateStrategyFragment(main, "otlp", map[string]interface{}{"endpoint": "collector:4318", "protocol": "http"}, false, nil)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !strings.HasSuffix(disabled, ".disabled") {
		t.Errorf("disabling must rename to .disabled, got %s", disabled)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the enabled file must be gone after disabling")
	}
	cfg, err := LoadFromDisk(main, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range cfg.Storage {
		if sc.Name == "otlp" {
			t.Error("a disabled output must not be loaded")
		}
	}
	frags, _ = ListStrategyFragments(main)
	for _, f := range frags {
		if f.Name == "otlp" && (f.Enabled || f.Params["protocol"] != "http") {
			t.Errorf("disabled fragment must still be listed with its params: %+v", f)
		}
	}

	enabled, err := UpdateStrategyFragment(main, "otlp", map[string]interface{}{"endpoint": "collector:4318"}, true, nil)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if enabled != path {
		t.Errorf("enabling must restore %s, got %s", path, enabled)
	}

	// The install's http file is taken over on its first console write.
	if _, err := UpdateStrategyFragment(main, "http", map[string]interface{}{"port": 9090, "endpoints": []interface{}{"web", "prtg"}}, true, nil); err != nil {
		t.Fatalf("update http: %v", err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "00-http.yaml"))
	if !strings.HasPrefix(string(raw), managedFragmentHeader) || !strings.Contains(string(raw), "port: 9090") {
		t.Errorf("http fragment must be rewritten in place, got:\n%s", raw)
	}

	if _, err := DeleteStrategyFragment(main, "otlp"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := DeleteStrategyFragment(main, "otlp"); err == nil {
		t.Error("deleting twice must fail")
	}
	if _, err := UpdateStrategyFragment(main, "nothing", nil, true, nil); err == nil {
		t.Error("updating an unknown output must fail")
	}
}
