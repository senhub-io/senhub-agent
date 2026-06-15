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
