package hyperv

import (
	"testing"
	"time"
)

func TestNewHypervProbe_DefaultInterval(t *testing.T) {
	probe, err := NewHypervProbe(map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if probe == nil {
		t.Fatal("expected non-nil probe")
	}
	if got := probe.GetInterval(); got != defaultInterval {
		t.Errorf("expected default interval %s, got %s", defaultInterval, got)
	}
}

func TestNewHypervProbe_CustomInterval(t *testing.T) {
	probe, err := NewHypervProbe(map[string]interface{}{"interval": 120}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := probe.GetInterval(); got != 120*time.Second {
		t.Errorf("expected 120s interval, got %s", got)
	}
}

func TestNewHypervProbe_ProbeType(t *testing.T) {
	probe, err := NewHypervProbe(map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// GetProbeType lives on the concrete *HypervProbe (embedded BaseProbe),
	// not on the types.Probe interface — use a type assertion to reach it.
	hp, ok := probe.(*HypervProbe)
	if !ok {
		t.Fatalf("expected *HypervProbe, got %T", probe)
	}
	if got := hp.GetProbeType(); got != ProbeType {
		t.Errorf("expected probe type %q, got %q", ProbeType, got)
	}
}

func TestHypervEntitySource_Observe_BeforeFirstCollect(t *testing.T) {
	src := newHypervEntitySource()
	_, ok := src.Observe()
	if ok {
		t.Error("expected ok=false before first collect")
	}
}

func TestHypervEntitySource_Observe_AfterSuccess(t *testing.T) {
	src := newHypervEntitySource()
	src.update(true)
	obs, ok := src.Observe()
	if !ok {
		t.Error("expected ok=true after successful update")
	}
	if len(obs.Entities) == 0 {
		t.Error("expected at least one entity")
	}
	e := obs.Entities[0]
	if e.Type != entityTypeServiceInstance {
		t.Errorf("expected entity type %q, got %q", entityTypeServiceInstance, e.Type)
	}
	if got := e.ID[entityIDServiceInstanceID]; got != hypervInstanceID {
		t.Errorf("expected instance id %q, got %v", hypervInstanceID, got)
	}
}

func TestHypervEntitySource_Observe_AfterFailure(t *testing.T) {
	src := newHypervEntitySource()
	src.update(true)  // first successful cycle
	src.update(false) // transient failure
	_, ok := src.Observe()
	if ok {
		t.Error("expected ok=false after failed update")
	}
}
