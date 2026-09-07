package probes_test

import (
	"testing"

	"senhub-agent.go/internal/agent/probes"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/network"
)

// A declared schema must describe a probe that exists, carry a display
// name and a docs page, and only default to values its own kind accepts.
func TestProbeSpecs_DescribeRegisteredProbes(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()
	specs := probes.RegisteredProbeSpecs()
	if len(specs) == 0 {
		t.Fatal("no probe spec registered; the imports above should register at least the host probes")
	}
	for _, s := range specs {
		if !registered[s.Type] {
			t.Errorf("spec %q describes a probe type that is not registered", s.Type)
		}
		if s.DisplayName == "" || s.DocsPath == "" {
			t.Errorf("spec %q needs a DisplayName and a DocsPath", s.Type)
		}
		if problems := s.CheckParams(map[string]interface{}{}); len(problems) != 0 && !hasRequired(s) {
			t.Errorf("spec %q rejects an empty params map without declaring a required key: %v", s.Type, problems)
		}
	}
}

func hasRequired(s probes.ProbeSpec) bool {
	for _, p := range s.Params {
		if p.Required {
			return true
		}
	}
	return false
}
