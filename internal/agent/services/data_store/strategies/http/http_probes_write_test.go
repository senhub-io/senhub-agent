package http

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func TestCheckGovernanceBlock(t *testing.T) {
	if err := checkGovernanceBlock(nil); err != nil {
		t.Fatalf("nil must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{}); err != nil {
		t.Fatalf("empty must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{
		"criticality": "high", "labels": map[string]interface{}{"application": "erp"},
	}); err != nil {
		t.Fatalf("a documented block must pass: %v", err)
	}
	if err := checkGovernanceBlock(map[string]interface{}{"colour": "red"}); err == nil {
		t.Error("an unknown key must be refused")
	}
	if err := checkGovernanceBlock(map[string]interface{}{"criticality": "urgent"}); err == nil {
		t.Error("a value outside the closed set must be refused")
	}
}

func TestProbeWriteRequestToConfigDropsAnEmptyGovernance(t *testing.T) {
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", Governance: map[string]interface{}{}}).toConfig(); cfg.Governance != nil {
		t.Error("an empty object must not be written to the fragment")
	}
	gov := map[string]interface{}{"criticality": "low"}
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", Governance: gov}).toConfig(); cfg.Governance["criticality"] != "low" {
		t.Error("a filled block must be carried to the fragment")
	}
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", CustomTags: map[string]string{}}).toConfig(); cfg.CustomTags != nil {
		t.Error("an empty custom_tags map must not be written")
	}
	if cfg := (probeWriteRequest{Name: "a", Type: "cpu", CustomTags: map[string]string{"env": "lab"}}).toConfig(); cfg.CustomTags["env"] != "lab" {
		t.Error("custom tags must be carried to the fragment")
	}
}

func TestIntervalOf(t *testing.T) {
	cases := []struct {
		params map[string]interface{}
		want   int
	}{
		{map[string]interface{}{"interval": 30}, 30},
		{map[string]interface{}{"interval": int64(45)}, 45},
		{map[string]interface{}{"interval": 15.0}, 15},
		{map[string]interface{}{"interval": "5m"}, 300},
		{map[string]interface{}{"interval": "soon"}, 60},
		{map[string]interface{}{}, 60},
	}
	for _, c := range cases {
		if got := intervalOf(c.params, 60); got != c.want {
			t.Errorf("%v: want %d, got %d", c.params, c.want, got)
		}
	}
}

func TestCatalogEntryOffPlatform(t *testing.T) {
	e := annotateCatalogEntry(spec.Probe{Type: "cpu", Platforms: []string{"plan9"}}, nil, "k")
	if e.Authorized || e.Reason != "plan9 only" {
		t.Errorf("a probe for another platform must be refused with the reason, got %+v", e)
	}
	if e := annotateCatalogEntry(spec.Probe{Type: "cpu"}, nil, "k"); !e.Authorized {
		t.Error("no platform list means every platform")
	}
	if platformReason([]string{"linux", "windows"}) != "Linux and Windows only" {
		t.Error("platform names are spelled for the operator")
	}
}

// A probe whose schema makes a secret required must stay editable once
// that secret is in the store: the form only ever showed it as "Stored"
// and does not re-send it.
func TestProbeUpdateKeepsWorkingWhenARequiredSecretIsStored(t *testing.T) {
	spec.Register(spec.Probe{Type: "pgtest", DisplayName: "PG test", Params: []spec.ParamSpec{
		{Key: "host", Kind: spec.KindString, Required: true},
		{Key: "password", Kind: spec.KindString, Required: true, Secret: true},
	}})
	router, dir := newOutputsTestRouter(t)
	base := "/api/test-agent-key"
	code, resp := doJSON(t, router, "POST", base+"/config/probes", map[string]interface{}{
		"name": "pg", "type": "pgtest", "params": map[string]interface{}{
			"host": "db1", "password": "s3cret",
		},
	})
	if code != 201 {
		t.Fatalf("create: %d %v", code, resp)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "probes.d", "50-pg.yaml"))
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the password must be sealed:\n%s", raw)
	}
	// The form re-sends everything but the stored password.
	code, resp = doJSON(t, router, "PUT", base+"/config/probes/pg", map[string]interface{}{
		"name": "pg", "type": "pgtest", "params": map[string]interface{}{
			"host": "db2",
		},
	})
	if code != 200 {
		t.Fatalf("a stored required secret must not block the edit: %d %v", code, resp)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "probes.d", "50-pg.yaml"))
	if !strings.Contains(string(raw), "db2") || !strings.Contains(string(raw), "${secret:pg.password}") {
		t.Errorf("the edit must land and the stored password stay referenced:\n%s", raw)
	}
	// Removing it explicitly is a different thing: the schema says the
	// probe cannot run without it, so that must be refused.
	code, _ = doJSON(t, router, "PUT", base+"/config/probes/pg", map[string]interface{}{
		"name": "pg", "type": "pgtest", "params": map[string]interface{}{
			"host": "db2", "password": nil,
		},
	})
	if code != 400 {
		t.Errorf("dropping a required secret must be refused, got %d", code)
	}
}
