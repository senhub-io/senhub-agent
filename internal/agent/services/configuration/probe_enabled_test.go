package configuration

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// An absent `enabled` key must mean enabled. This is the whole reason the field
// is a pointer: with a plain bool the zero value is false, and every existing
// configuration — none of which carries the key — would go dark on the upgrade
// that introduced it.
func TestProbeConfig_AbsentEnabledMeansOn(t *testing.T) {
	if !(ProbeConfig{Name: "cpu", Type: "cpu"}).IsEnabled() {
		t.Fatal("a probe with no enabled key is off; every existing config would stop collecting")
	}
}

func TestProbeConfig_ExplicitFalseMeansOff(t *testing.T) {
	off := false
	on := true
	if (ProbeConfig{Name: "cpu", Enabled: &off}).IsEnabled() {
		t.Error("enabled: false did not disable the probe")
	}
	if !(ProbeConfig{Name: "cpu", Enabled: &on}).IsEnabled() {
		t.Error("enabled: true disabled the probe")
	}
}

// The switch must survive the YAML round trip in both layouts, since probes.d
// files are decoded straight into this struct.
func TestProbeConfig_EnabledRoundTripsThroughYAML(t *testing.T) {
	var batch []ProbeConfig
	if err := yaml.Unmarshal([]byte("- name: cpu\n  type: cpu\n- name: mysql\n  type: mysql\n  enabled: false\n"), &batch); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("got %d probes, want 2", len(batch))
	}
	if !batch[0].IsEnabled() {
		t.Error("the probe without the key was read as disabled")
	}
	if batch[1].IsEnabled() {
		t.Error("enabled: false was not read")
	}
}
