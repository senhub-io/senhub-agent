//go:build linux

package systemd

import (
	"testing"
	"time"
)

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig({}) returned error: %v", err)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("default interval = %v; want 30s", cfg.Interval)
	}
	for _, typ := range defaultIncludeTypes {
		if !cfg.IncludeTypes[typ] {
			t.Errorf("default include_types missing %q", typ)
		}
	}
	if len(cfg.Units) != 0 {
		t.Errorf("default units should be empty, got %v", cfg.Units)
	}
}

func TestParseConfig_ExplicitUnits(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"units":    []interface{}{"nginx.service", "*.timer"},
		"interval": 60,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Units) != 2 {
		t.Errorf("units count = %d; want 2", len(cfg.Units))
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("interval = %v; want 60s", cfg.Interval)
	}
}

func TestParseConfig_IncludeTypes(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"include_types": []interface{}{"service"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IncludeTypes["service"] {
		t.Error("include_types should contain service")
	}
	if cfg.IncludeTypes["timer"] {
		t.Error("include_types should not contain timer when explicitly set to service only")
	}
}

func TestUnitTypeSuffix(t *testing.T) {
	cases := []struct {
		name     string
		expected string
	}{
		{"nginx.service", "service"},
		{"logrotate.timer", "timer"},
		{"sys-fs-fuse-connections.mount", "mount"},
		{"dbus.socket", "socket"},
		{"noextension", ""},
	}
	for _, c := range cases {
		got := unitTypeSuffix(c.name)
		if got != c.expected {
			t.Errorf("unitTypeSuffix(%q) = %q; want %q", c.name, got, c.expected)
		}
	}
}
