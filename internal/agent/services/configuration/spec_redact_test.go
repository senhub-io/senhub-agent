package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskSpecPath(t *testing.T) {
	// (i) nested scalar path
	m1 := map[string]interface{}{
		"auth": map[string]interface{}{"password": "p1", "user": "u"},
	}
	maskSpecPath(m1, []string{"auth", "password"})
	auth := m1["auth"].(map[string]interface{})
	if auth["password"] != "***" {
		t.Errorf("auth.password = %v, want ***", auth["password"])
	}
	if auth["user"] != "u" {
		t.Errorf("auth.user = %v, want u", auth["user"])
	}

	// (ii) path through a list of maps
	m2 := map[string]interface{}{
		"v3": map[string]interface{}{
			"users": []interface{}{
				map[string]interface{}{"auth_password": "a1"},
				map[string]interface{}{"auth_password": "a2"},
			},
		},
	}
	maskSpecPath(m2, []string{"v3", "users", "auth_password"})
	users := m2["v3"].(map[string]interface{})["users"].([]interface{})
	for i, u := range users {
		if got := u.(map[string]interface{})["auth_password"]; got != "***" {
			t.Errorf("users[%d].auth_password = %v, want ***", i, got)
		}
	}

	// (iii) whole secret map: values masked, keys kept
	m3 := map[string]interface{}{
		"headers": map[string]interface{}{"Cookie": "c", "X": "x"},
	}
	maskSpecPath(m3, []string{"headers"})
	headers := m3["headers"].(map[string]interface{})
	if headers["Cookie"] != "***" || headers["X"] != "***" {
		t.Errorf("headers = %v, want all values ***", headers)
	}
	if _, ok := headers["Cookie"]; !ok {
		t.Errorf("header key Cookie was dropped")
	}

	// (iv) missing path: no change, no panic
	m4 := map[string]interface{}{"a": "b"}
	maskSpecPath(m4, []string{"missing", "path"})
	if m4["a"] != "b" {
		t.Errorf("m4 changed on missing path: %v", m4)
	}
}

func TestURLUserinfoMaskedUnderAnyKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "probes.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("config_version: 3\nagent:\n  name: t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := "- name: h\n  type: http_check\n  params:\n    targets:\n      - http://user:url-s3cret@host.example/health\n"
	if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-h.yaml"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForShow(filepath.Join(dir, "agent.yaml"), ShowRedact, nil)
	if err != nil {
		t.Fatalf("LoadForShow: %v", err)
	}
	out, err := MarshalSortedYAML(cfg)
	if err != nil {
		t.Fatalf("MarshalSortedYAML: %v", err)
	}
	if strings.Contains(string(out), "url-s3cret") {
		t.Errorf("URL password leaked in redacted show:\n%s", out)
	}
	if !strings.Contains(string(out), "host.example/health") {
		t.Errorf("URL host path missing from redacted show:\n%s", out)
	}
}
