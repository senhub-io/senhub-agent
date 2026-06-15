// Cross-platform tests for the pure helpers in helpers.go.
// No build tag: these run on every OS (darwin, linux, windows).
// No live D-Bus connection is required — all inputs are constructed
// inline from dbus.UnitStatus literals.
package systemd

import (
	"testing"
	"time"

	dbus "github.com/coreos/go-systemd/v22/dbus"
)

// ---------------------------------------------------------------------------
// parseConfig
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// unitTypeSuffix
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// filterUnits
// ---------------------------------------------------------------------------

func makeUnit(name, active, sub, load string) dbus.UnitStatus {
	return dbus.UnitStatus{
		Name:        name,
		ActiveState: active,
		SubState:    sub,
		LoadState:   load,
	}
}

func TestFilterUnits_AllTypes_WhenUnitsEmpty(t *testing.T) {
	cfg := probeConfig{
		IncludeTypes: map[string]bool{"service": true, "timer": true},
	}
	all := []dbus.UnitStatus{
		makeUnit("nginx.service", "active", "running", "loaded"),
		makeUnit("logrotate.timer", "active", "waiting", "loaded"),
		makeUnit("dbus.socket", "active", "running", "loaded"),
	}
	got := filterUnits(cfg, all)
	if len(got) != 2 {
		t.Errorf("filterUnits: got %d units; want 2 (service+timer only)", len(got))
	}
}

func TestFilterUnits_GlobPattern(t *testing.T) {
	cfg := probeConfig{
		Units:        []string{"nginx.*", "ssh.service"},
		IncludeTypes: map[string]bool{"service": true, "socket": true},
	}
	all := []dbus.UnitStatus{
		makeUnit("nginx.service", "active", "running", "loaded"),
		makeUnit("nginx.socket", "active", "listening", "loaded"),
		makeUnit("ssh.service", "active", "running", "loaded"),
		makeUnit("cron.service", "active", "running", "loaded"),
	}
	got := filterUnits(cfg, all)
	if len(got) != 3 {
		t.Errorf("filterUnits with globs: got %d; want 3", len(got))
	}
}

func TestFilterUnits_ExcludesUnknownType(t *testing.T) {
	cfg := probeConfig{
		IncludeTypes: map[string]bool{"service": true},
	}
	all := []dbus.UnitStatus{
		makeUnit("foo.service", "active", "running", "loaded"),
		makeUnit("bar.scope", "active", "running", "loaded"),
		makeUnit("baz.path", "active", "waiting", "loaded"),
	}
	got := filterUnits(cfg, all)
	if len(got) != 1 || got[0].Name != "foo.service" {
		t.Errorf("filterUnits: expected only foo.service, got %v", got)
	}
}

func TestFilterUnits_EmptyInput(t *testing.T) {
	cfg := probeConfig{
		IncludeTypes: map[string]bool{"service": true},
	}
	got := filterUnits(cfg, nil)
	if len(got) != 0 {
		t.Errorf("filterUnits(nil): got %d; want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// buildDatapoints
// ---------------------------------------------------------------------------

func TestBuildDatapoints_ActiveRunningLoaded(t *testing.T) {
	u := makeUnit("nginx.service", "active", "running", "loaded")
	ts := time.Unix(1000, 0)
	pts := buildDatapoints(u, ts, nil)

	// Expect 3 datapoints (no NRestarts passed).
	if len(pts) != 3 {
		t.Fatalf("buildDatapoints: got %d points; want 3", len(pts))
	}

	byName := make(map[string]float64, len(pts))
	for _, p := range pts {
		byName[p.Name] = p.Value
	}

	if byName["systemd.unit.active_state"] != 1 {
		t.Errorf("active_state = %v; want 1", byName["systemd.unit.active_state"])
	}
	if byName["systemd.unit.sub_state"] != 1 {
		t.Errorf("sub_state = %v; want 1 (running)", byName["systemd.unit.sub_state"])
	}
	if byName["systemd.unit.load_state"] != 1 {
		t.Errorf("load_state = %v; want 1 (loaded)", byName["systemd.unit.load_state"])
	}
}

func TestBuildDatapoints_InactiveDeadNotFound(t *testing.T) {
	u := makeUnit("failed.service", "inactive", "dead", "not-found")
	pts := buildDatapoints(u, time.Now(), nil)

	byName := make(map[string]float64, len(pts))
	for _, p := range pts {
		byName[p.Name] = p.Value
	}

	if byName["systemd.unit.active_state"] != 0 {
		t.Errorf("active_state = %v; want 0", byName["systemd.unit.active_state"])
	}
	if byName["systemd.unit.sub_state"] != 0 {
		t.Errorf("sub_state = %v; want 0 (dead)", byName["systemd.unit.sub_state"])
	}
	if byName["systemd.unit.load_state"] != 0 {
		t.Errorf("load_state = %v; want 0 (not-found)", byName["systemd.unit.load_state"])
	}
}

func TestBuildDatapoints_WithNRestarts(t *testing.T) {
	u := makeUnit("crashed.service", "active", "running", "loaded")
	restarts := float64(3)
	pts := buildDatapoints(u, time.Now(), &restarts)

	// Expect 4 datapoints (3 states + restarts).
	if len(pts) != 4 {
		t.Fatalf("buildDatapoints with restarts: got %d points; want 4", len(pts))
	}
	byName := make(map[string]float64, len(pts))
	for _, p := range pts {
		byName[p.Name] = p.Value
	}
	if byName["systemd.unit.restarts"] != 3 {
		t.Errorf("restarts = %v; want 3", byName["systemd.unit.restarts"])
	}
}

func TestBuildDatapoints_TimerNoRestarts(t *testing.T) {
	// Timer units must never emit a restarts datapoint, even when a
	// non-nil nRestarts is supplied (the helper guards on unit type).
	u := makeUnit("logrotate.timer", "active", "waiting", "loaded")
	restarts := float64(0)
	pts := buildDatapoints(u, time.Now(), &restarts)

	for _, p := range pts {
		if p.Name == "systemd.unit.restarts" {
			t.Errorf("timer unit emitted systemd.unit.restarts — unexpected")
		}
	}
}

func TestBuildDatapoints_SubStateTags(t *testing.T) {
	u := makeUnit("nginx.service", "active", "running", "loaded")
	pts := buildDatapoints(u, time.Now(), nil)

	for _, p := range pts {
		if p.Name != "systemd.unit.sub_state" {
			continue
		}
		for _, tag := range p.Tags {
			if tag.Key == "sub_state" && tag.Value == "running" {
				return
			}
		}
		t.Error("sub_state datapoint missing sub_state=running tag")
	}
}

func TestBuildDatapoints_UnitTypeTags(t *testing.T) {
	cases := []struct {
		unitName string
		wantType string
	}{
		{"nginx.service", "service"},
		{"logrotate.timer", "timer"},
		{"dbus.socket", "socket"},
	}
	for _, c := range cases {
		u := makeUnit(c.unitName, "active", "running", "loaded")
		pts := buildDatapoints(u, time.Now(), nil)
		for _, p := range pts {
			if p.Name == "systemd.unit.sub_state" {
				continue
			}
			for _, tag := range p.Tags {
				if tag.Key == "systemd.unit.type" {
					if tag.Value != c.wantType {
						t.Errorf("%s: systemd.unit.type = %q; want %q", c.unitName, tag.Value, c.wantType)
					}
					goto nextCase
				}
			}
		}
	nextCase:
	}
}

func TestBuildDatapoints_ListeningSubState(t *testing.T) {
	u := makeUnit("dbus.socket", "active", "listening", "loaded")
	pts := buildDatapoints(u, time.Now(), nil)
	byName := make(map[string]float64, len(pts))
	for _, p := range pts {
		byName[p.Name] = p.Value
	}
	if byName["systemd.unit.sub_state"] != 1 {
		t.Errorf("sub_state for listening socket = %v; want 1", byName["systemd.unit.sub_state"])
	}
}
