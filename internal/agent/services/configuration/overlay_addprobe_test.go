package configuration

import (
	"testing"

	"senhub-agent.go/internal/agent/services/extension"
)

func addProbeTestConfig() *LocalConfigurationData {
	enabled := true
	return &LocalConfigurationData{
		Probes: []ProbeConfig{
			{Name: "local", Type: "http", Enabled: &enabled, Params: ProbeConfigParams{}},
		},
	}
}

func TestOverlayAddProbeSuccess(t *testing.T) {
	cfg := addProbeTestConfig()
	target := &overlayTarget{cfg: cfg}
	ok := target.AddProbe(extension.ManagedProbe{
		Name:     "managed",
		Type:     "snmp_poll",
		Enabled:  true,
		Interval: 60,
		Params:   map[string]any{"community": "public"},
	})
	if !ok {
		t.Fatal("AddProbe refused a valid managed probe")
	}
	if len(cfg.Probes) != 2 {
		t.Fatalf("len(Probes) = %d, want 2", len(cfg.Probes))
	}
	added := cfg.Probes[1]
	if added.Name != "managed" || added.Type != "snmp_poll" {
		t.Fatalf("added probe = %q/%q, want managed/snmp_poll", added.Name, added.Type)
	}
	if added.Enabled == nil || !*added.Enabled {
		t.Fatal("added probe Enabled is not true")
	}
	if got := added.Params["interval"]; got != 60 {
		t.Fatalf("params.interval = %v, want 60", got)
	}
	if got := added.Params["community"]; got != "public" {
		t.Fatalf("params.community = %v, want public", got)
	}
}

func TestOverlayAddProbeZeroIntervalOmitsParam(t *testing.T) {
	cfg := addProbeTestConfig()
	target := &overlayTarget{cfg: cfg}
	if !target.AddProbe(extension.ManagedProbe{Name: "m", Type: "http"}) {
		t.Fatal("AddProbe refused a valid probe with zero interval")
	}
	if _, ok := cfg.Probes[1].Params["interval"]; ok {
		t.Fatal("params.interval set for a zero Interval")
	}
}

func TestOverlayAddProbeDuplicateNameRefused(t *testing.T) {
	cfg := addProbeTestConfig()
	target := &overlayTarget{cfg: cfg}
	if target.AddProbe(extension.ManagedProbe{Name: "local", Type: "http"}) {
		t.Fatal("AddProbe accepted a duplicate name")
	}
	if len(cfg.Probes) != 1 {
		t.Fatalf("len(Probes) = %d, want 1", len(cfg.Probes))
	}
}

func TestOverlayAddProbeBadIntervalRefused(t *testing.T) {
	for _, interval := range []int{-5, 5, 3601, 4000} {
		cfg := addProbeTestConfig()
		target := &overlayTarget{cfg: cfg}
		if target.AddProbe(extension.ManagedProbe{Name: "m", Type: "http", Interval: interval}) {
			t.Fatalf("AddProbe accepted interval %d", interval)
		}
		if len(cfg.Probes) != 1 {
			t.Fatalf("interval %d changed the probe list", interval)
		}
	}
}

func TestOverlayAddProbeEmptyNameOrTypeRefused(t *testing.T) {
	cfg := addProbeTestConfig()
	target := &overlayTarget{cfg: cfg}
	if target.AddProbe(extension.ManagedProbe{Name: "", Type: "http"}) {
		t.Fatal("AddProbe accepted an empty name")
	}
	if target.AddProbe(extension.ManagedProbe{Name: "m", Type: ""}) {
		t.Fatal("AddProbe accepted an empty type")
	}
	if len(cfg.Probes) != 1 {
		t.Fatalf("len(Probes) = %d, want 1", len(cfg.Probes))
	}
}

func TestOverlayAddProbeRollbackOnPanic(t *testing.T) {
	extension.ResetForTest()
	defer extension.ResetForTest()
	extension.Register(extension.Extension{
		Name: "panicker",
		New:  func(extension.Host) (extension.Service, error) { return nil, nil },
		Overlay: func(t extension.OverlayTarget) {
			t.AddProbe(extension.ManagedProbe{Name: "managed", Type: "http"})
			panic("boom")
		},
	})
	cfg := addProbeTestConfig()
	applyOverlays(cfg, "")
	if len(cfg.Probes) != 1 {
		t.Fatalf("len(Probes) = %d after panicking overlay, want 1", len(cfg.Probes))
	}
	if cfg.Probes[0].Name != "local" {
		t.Fatalf("remaining probe = %q, want local", cfg.Probes[0].Name)
	}
}

func TestOverlayAddProbeIntervalInParamsRefused(t *testing.T) {
	cfg := addProbeTestConfig()
	target := &overlayTarget{cfg: cfg}
	if target.AddProbe(extension.ManagedProbe{Name: "m", Type: "http", Params: map[string]any{"interval": 1}}) {
		t.Fatal("AddProbe accepted an interval smuggled in Params")
	}
	if len(cfg.Probes) != 1 {
		t.Fatalf("probes = %d, want 1", len(cfg.Probes))
	}
}
