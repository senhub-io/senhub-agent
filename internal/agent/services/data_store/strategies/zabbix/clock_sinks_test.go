package zabbix

import (
	"bytes"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http/prometheus"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// The host clock reaches Prometheus as a gauge a `time() - metric`
// expression can use, Zabbix as an item, and the template compares it
// with the server's time.
func TestHostClockReachesEverySink(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def := defs["cpu"]
	cm := otelmapper.CacheMetric{ProbeName: "cpu", ProbeType: "cpu", MetricName: "system_time", Value: 1_800_000_000.25}

	recs, err := otelmapper.Resolve(&def, cm, otelmapper.DefaultResolveOptions())
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v %+v", err, recs)
	}
	if r := recs[0]; r.Name != "senhub.system.time" || r.Type != "gauge" || r.Unit != "s" || r.Value != 1_800_000_000.25 {
		t.Fatalf("record = %+v", r)
	}

	var prom bytes.Buffer
	if err := prometheus.SerializeToTextExposition(recs, &prom, prometheus.SerializeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prom.String(), "# TYPE senhub_system_time_seconds gauge") ||
		!strings.Contains(prom.String(), "} 1.80000000025e+09") {
		t.Errorf("prometheus output:\n%s", prom.String())
	}

	items := itemsFor("senhub", &def, cm)
	if len(items) != 1 || items[0].Key != "senhub.system.time[cpu]" {
		t.Errorf("zabbix items = %+v", items)
	}

	for _, version := range []string{"6.0", "7.0"} {
		exp, err := template.Generate(def, template.Options{Version: version, Platform: "linux"})
		if err != nil {
			t.Fatal(err)
		}
		tpl := exp.ZabbixExport.Templates[0]
		var found bool
		for _, r := range tpl.DiscoveryRules {
			for _, p := range r.ItemPrototypes {
				if !strings.HasPrefix(p.Key, "senhub.system.time[") {
					continue
				}
				if p.Units != "unixtime" {
					t.Errorf("%s: units = %q, want unixtime", version, p.Units)
				}
				for _, tr := range p.TriggerPrototypes {
					want := "fuzzytime(/" + tpl.Template + "/" + p.Key + ",{$SENHUB.CLOCK.DRIFT.MAX})=0"
					if tr.Expression == want {
						found = true
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no fuzzytime drift trigger on the host clock", version)
		}
		var macro bool
		for _, m := range tpl.Macros {
			if m.Macro == template.ClockDriftMacro && m.Value == "60s" {
				macro = true
			}
		}
		if !macro {
			t.Errorf("%s: the drift macro is missing from %+v", version, tpl.Macros)
		}
	}
}
