package zabbix

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// The whole point of generating templates from the definitions is that
// a prototype, once the server has substituted its macros, is the key the
// agent sends. This pins the two builders together on real definitions.
func TestGeneratedPrototypesNameTheKeysTheAgentSends(t *testing.T) {
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
		protos := map[string]bool{}
		rules := map[string]bool{}
		for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
			rules[r.Key] = true
			for _, p := range r.ItemPrototypes {
				protos[p.Key] = true
			}
		}
		for _, m := range def.Metrics {
			if m.Otel != nil && m.Otel.Skip {
				continue
			}
			labels := dimensions(&def, &m)
			tags := map[string]string{}
			for i, l := range labels {
				tags[l] = "v" + string(rune('0'+i))
			}
			cm := otelmapper.CacheMetric{ProbeName: "inst", ProbeType: def.ProbeName, MetricName: m.Name, Value: 1, Tags: tags}
			// A metric declared as a distribution arrives as one, and
			// the agent then sends the two parts rather than the
			// metric's own name. The guard has to send what it would
			// really send, or it proves nothing about those keys.
			if m.Otel != nil && m.Otel.Distribution {
				sum := 1.0
				cm.Histogram = &datapoint.HistogramValue{Count: 1, Sum: &sum}
			}
			for _, sent := range sentKeys("senhub", &def, m, cm) {

				// A metric of a variant family is named by a prototype whose
				// key carries the attribute as a macro, because the agent
				// discovers which values it feeds instead of the generator
				// declaring them all.
				var famMacros, famValues []string
				if fam := familiesOf(&def)[m.Name]; fam != nil {
					famMacros = attrMacros(fam.labels, fam.attrKeys)
					famValues = staticAttributeValues(&m)
				}

				// Substitute the macros of every prototype and look for the sent key.
				found := false
				for p := range protos {
					candidate := strings.ReplaceAll(p, probeMacro, "inst")
					for _, l := range labels {
						candidate = strings.ReplaceAll(candidate, macroFor(l), tags[l])
					}
					for i, mac := range famMacros {
						if i < len(famValues) {
							candidate = strings.ReplaceAll(candidate, mac, famValues[i])
						}
					}
					if candidate == sent {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s/%s: the agent sends %s but no generated prototype names it", def.ProbeName, m.Name, sent)
				}
			}
			wantRule := discoveryKey("senhub", def.ProbeName, labels)
			if fam := familiesOf(&def)[m.Name]; fam != nil {
				wantRule = variantRuleKey("senhub", def.ProbeName, fam.otelName, labels)
			}
			if !rules[wantRule] {
				t.Errorf("%s/%s: the agent serves discovery %s but the template has no such rule", def.ProbeName, m.Name, wantRule)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d metrics checked; the embedded definitions should give far more", checked)
	}
}

// sentKeys is every key the agent sends for one metric: its own, or the
// parts of a distribution.
func sentKeys(prefix string, def *transformers.ProbeDefinition, m transformers.MetricDefinition, cm otelmapper.CacheMetric) []string {
	items := itemsFor(prefix, def, cm)
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	return keys
}
