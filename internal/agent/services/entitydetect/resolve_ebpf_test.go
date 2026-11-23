package entitydetect

import (
	"testing"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestResolveEBPFDefaultsOn(t *testing.T) {
	if !Resolve(nil, nil, "agent").EBPFEnabled {
		t.Fatal("no entities block: the helper client must default to on")
	}
	if !Resolve(&configuration.EntitiesConfig{Enabled: true}, nil, "agent").EBPFEnabled {
		t.Fatal("an entities block without ebpf must keep the default")
	}
	if !Resolve(&configuration.EntitiesConfig{Enabled: true, EBPF: &configuration.EntitiesEBPFConfig{}}, nil, "agent").EBPFEnabled {
		t.Fatal("an ebpf block without enabled must keep the default")
	}
}

func TestResolveEBPFCanBeSwitchedOff(t *testing.T) {
	off := false
	cfg := Resolve(&configuration.EntitiesConfig{Enabled: true, EBPF: &configuration.EntitiesEBPFConfig{Enabled: &off}}, nil, "agent")
	if cfg.EBPFEnabled {
		t.Fatal("entities.ebpf.enabled: false must switch the client off")
	}
}

func TestEBPFSwitchParsesFromYAML(t *testing.T) {
	var e configuration.EntitiesConfig
	if err := yaml.Unmarshal([]byte("enabled: true\nebpf:\n  enabled: false\n"), &e); err != nil {
		t.Fatal(err)
	}
	if e.EBPF == nil || e.EBPF.Enabled == nil || *e.EBPF.Enabled {
		t.Fatalf("%+v", e.EBPF)
	}
}
