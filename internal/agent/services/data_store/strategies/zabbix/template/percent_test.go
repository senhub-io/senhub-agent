package template

import (
	"strings"
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
	u := got["senhub.system.memory.utilization[{#PROBE_MEMORY}]"]
	if u.Units != "%" || len(u.Preprocessing) != 1 || u.Preprocessing[0].Type != "MULTIPLIER" || u.Preprocessing[0].Parameters[0] != "100" {
		t.Errorf("utilization prototype = units %q, preprocessing %v; want %% and a multiplier of 100", u.Units, u.Preprocessing)
	}
	c := got["senhub.system.processes.count[{#PROBE_MEMORY}]"]
	if c.Units != "" || len(c.Preprocessing) != 0 {
		t.Errorf("a dimensionless count must stay as sent; got units %q, preprocessing %v", c.Units, c.Preprocessing)
	}
}

// A cumulative monotonic sum is stored and shown as a per-second rate;
// gauges and non-monotonic sums stay as sent.
func TestCumulativeSumsAreShownAsRates(t *testing.T) {
	def := transformers.ProbeDefinition{
		ProbeName: "network",
		Metrics: []transformers.MetricDefinition{
			{Name: "rx_bytes", DisplayName: "Received", Unit: "B",
				Otel: &transformers.OtelMapping{Name: "system.network.io", Unit: "By", Type: "counter"}},
			{Name: "rx_packets", DisplayName: "Packets", Unit: "#",
				Otel: &transformers.OtelMapping{Name: "system.network.packet.count", Unit: "{packet}", Type: "counter"}},
			{Name: "cpu_time", DisplayName: "CPU time", Unit: "s",
				Otel: &transformers.OtelMapping{Name: "system.cpu.time", Unit: "s", Type: "counter"}},
			{Name: "connections", DisplayName: "Connections", Unit: "#",
				Otel: &transformers.OtelMapping{Name: "system.network.connection.count", Unit: "{connection}", Type: "updowncounter"}},
			{Name: "mem_used", DisplayName: "Used", Unit: "B",
				Otel: &transformers.OtelMapping{Name: "system.memory.usage", Unit: "By", Type: "gauge"}},
		},
	}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		units string
		rate  bool
	}
	wants := map[string]want{
		"system.network.io":               {"Bps", true},
		"system.network.packet.count":     {"/s", true},
		"system.cpu.time":                 {"s/s", true},
		"system.network.connection.count": {"", false},
		"system.memory.usage":             {"B", false},
	}
	seen := 0
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		for _, p := range r.ItemPrototypes {
			for name, w := range wants {
				if !strings.HasPrefix(p.Key, "senhub."+name+"[") {
					continue
				}
				seen++
				rated := len(p.Preprocessing) == 1 && p.Preprocessing[0].Type == "CHANGE_PER_SECOND"
				if rated != w.rate || p.Units != w.units || (!w.rate && len(p.Preprocessing) != 0) {
					t.Errorf("%s: units %q, preprocessing %v; want units %q, rate %v", p.Key, p.Units, p.Preprocessing, w.units, w.rate)
				}
			}
		}
	}
	if seen != len(wants) {
		t.Errorf("matched %d prototypes, want %d", seen, len(wants))
	}
}
