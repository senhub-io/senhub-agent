package zabbix

import (
	"encoding/json"
	"regexp"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type oneDefinition struct{ def *transformers.ProbeDefinition }

func (o oneDefinition) GetProbeDefinition(string) *transformers.ProbeDefinition { return o.def }

// fedRows runs the agent's discovery over one series per metric of the
// definition, all on the same instance, minus the metric named in skip.
func fedRows(def *transformers.ProbeDefinition, skip string) map[string]string {
	var metrics []otelmapper.CacheMetric
	for _, m := range def.Metrics {
		if (m.Otel != nil && m.Otel.Skip) || m.Name == skip {
			continue
		}
		m := m
		tags := map[string]string{}
		for i, l := range dimensions(def, &m) {
			tags[l] = "v" + string(rune('0'+i))
		}
		cm := otelmapper.CacheMetric{ProbeName: "inst", ProbeType: def.ProbeName, MetricName: m.Name, Value: 1, Tags: tags}
		if m.Otel != nil && m.Otel.Distribution {
			sum := 1.0
			cm.Histogram = &datapoint.HistogramValue{Count: 1, Sum: &sum}
		}
		metrics = append(metrics, cm)
	}
	out := map[string]string{} // rule key -> FedMacro value of its row
	for _, it := range discoveryItems("senhub", oneDefinition{def}, metrics) {
		var rows []map[string]string
		if err := json.Unmarshal([]byte(it.Value), &rows); err != nil || len(rows) == 0 {
			continue
		}
		out[it.Key] = rows[0][template.FedMacro]
	}
	return out
}

// The agent and the template must agree on what "fed" means: an instance
// sending every metric of a rule keeps every item, and an instance
// missing one metric loses that item and no other. This drives every
// embedded definition through both sides, so a change to the id on one
// side cannot silently switch items off, or leave empty ones behind.
func TestAnInstanceGetsItemsForExactlyTheMetricsItFeeds(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, def := range defs {
		def := def
		exp, err := template.Generate(def, template.Options{Prefix: "senhub"})
		if err != nil {
			t.Fatalf("%s: %v", def.ProbeName, err)
		}
		all := fedRows(&def, "")
		for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
			for _, o := range r.Overrides {
				if len(o.Filter.Conditions) != 2 || o.Filter.Conditions[0].Macro != template.FedMacro {
					continue
				}
				re := regexp.MustCompile(o.Filter.Conditions[1].Value)
				fed, ok := all[r.Key]
				if !ok {
					t.Errorf("%s: rule %s has no discovery row from the agent", def.ProbeName, r.Key)
					continue
				}
				if !re.MatchString(fed) {
					t.Errorf("%s: %q would drop %q on an instance that feeds every metric (fed %s)",
						def.ProbeName, r.Key, o.Operations[0].Value, fed)
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no override was checked; the guard proves nothing")
	}

	// And the other way: an instance that does not send a metric loses
	// that item. The network speed of a virtio NIC is the case measured.
	var network transformers.ProbeDefinition
	for _, d := range defs {
		if d.ProbeName == "network" {
			network = d
		}
	}
	var speed *transformers.MetricDefinition
	for i, m := range network.Metrics {
		if m.Otel != nil && m.Otel.Name == "senhub.system.network.interface.speed" {
			speed = &network.Metrics[i]
		}
	}
	if speed == nil {
		t.Fatal("the network definition declares no interface speed")
	}
	without := fedRows(&network, speed.Name)
	id := template.FedID(*speed)
	for rule, fed := range without {
		if regexp.MustCompile(","+regexp.QuoteMeta(id)+",").MatchString(fed) {
			t.Errorf("rule %s still lists %s for an interface that does not send it", rule, id)
		}
	}
}
