package template

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// A utilization is stored and shown as a percentage, as the native agent
// shows it; the key and what the agent sends are unchanged.
func TestUtilizationIsShownAsAPercentage(t *testing.T) {
	def := transformers.ProbeDefinition{
		ProbeName: "memory",
		Metrics: []transformers.MetricDefinition{
			{Name: "memory_used_percent", DisplayName: "Memory Usage", Unit: "%",
				Otel: &transformers.OtelMapping{Name: "system.memory.utilization", Unit: "1", Type: "gauge"}},
			{Name: "processes", DisplayName: "Processes", Unit: "#",
				Otel: &transformers.OtelMapping{Name: "system.processes.count", Unit: "1", Type: "gauge"}},
		},
	}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ItemPrototype{}
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		for _, p := range r.ItemPrototypes {
			got[p.Key] = p
		}
	}
	u := got["senhub.system.memory.utilization[{#PROBE}]"]
	if u.Units != "%" || len(u.Preprocessing) != 1 || u.Preprocessing[0].Type != "MULTIPLIER" || u.Preprocessing[0].Parameters[0] != "100" {
		t.Errorf("utilization prototype = units %q, preprocessing %v; want %% and a multiplier of 100", u.Units, u.Preprocessing)
	}
	c := got["senhub.system.processes.count[{#PROBE}]"]
	if c.Units != "" || len(c.Preprocessing) != 0 {
		t.Errorf("a dimensionless count must stay as sent; got units %q, preprocessing %v", c.Units, c.Preprocessing)
	}
}
