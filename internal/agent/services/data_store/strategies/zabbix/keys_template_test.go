package zabbix

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
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
			sent := itemFor("senhub", &def, cm).Key

			// Substitute the macros of every prototype and look for the sent key.
			found := false
			for p := range protos {
				candidate := strings.ReplaceAll(p, probeMacro, "inst")
				for _, l := range labels {
					candidate = strings.ReplaceAll(candidate, macroFor(l), tags[l])
				}
				if candidate == sent {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s/%s: the agent sends %s but no generated prototype names it", def.ProbeName, m.Name, sent)
			}
			if !rules[discoveryKey("senhub", def.ProbeName, labels)] {
				t.Errorf("%s/%s: the agent serves discovery %s but the template has no such rule", def.ProbeName, m.Name, discoveryKey("senhub", def.ProbeName, labels))
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d metrics checked; the embedded definitions should give far more", checked)
	}
}
