package template

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func graphDefinition() transformers.ProbeDefinition {
	return transformers.ProbeDefinition{
		ProbeName:    "net",
		FriendlyName: "Net",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "rx", DisplayName: "Net {iface} Received", MultiInstanceLabels: []string{"iface"},
				Otel: &transformers.OtelMapping{Name: "system.network.io", Unit: "By", Type: "gauge", Attributes: map[string]string{"network.io.direction": "receive"}},
			},
			{
				Name: "tx", DisplayName: "Net {iface} Sent", MultiInstanceLabels: []string{"iface"},
				Otel: &transformers.OtelMapping{Name: "system.network.io", Unit: "By", Type: "gauge", Attributes: map[string]string{"network.io.direction": "transmit"}},
			},
			{
				Name: "speed", DisplayName: "Net {iface} Speed", MultiInstanceLabels: []string{"iface"},
				Otel: &transformers.OtelMapping{Name: "senhub.system.network.speed", Unit: "By/s", Type: "gauge"},
			},
			{
				Name: "winonly", DisplayName: "Net {iface} Win", MultiInstanceLabels: []string{"iface"}, Platforms: []string{"windows"},
				Otel: &transformers.OtelMapping{Name: "senhub.system.network.win", Unit: "1", Type: "gauge"},
			},
		},
		Graphs: []transformers.GraphDefinition{
			{Name: "Traffic", Series: []string{"rx", "tx"}},
			{Name: "Speed", Series: []string{"speed", "winonly"}},
			{Name: "Only Windows", Series: []string{"winonly"}},
		},
	}
}

func TestAGraphIsDrawnOverTheItemsOfItsRule(t *testing.T) {
	exp, err := Generate(graphDefinition(), Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	var names []string
	for _, r := range tpl.DiscoveryRules {
		keys := map[string]bool{}
		for _, p := range r.ItemPrototypes {
			keys[p.Key] = true
		}
		for _, g := range r.GraphPrototypes {
			names = append(names, g.Name)
			for _, gi := range g.GraphItems {
				if !keys[gi.Item.Key] {
					t.Errorf("graph %q draws %q, which is not an item prototype of its rule", g.Name, gi.Item.Key)
				}
				if gi.Item.Host != tpl.Template {
					t.Errorf("graph item host = %q, want the template %q", gi.Item.Host, tpl.Template)
				}
			}
		}
	}
	joined := strings.Join(names, "|")
	// Both directions are one prototype of a family, drawn once, and the
	// chart is named with the macros of its rule so each instance gets
	// its own.
	if !strings.Contains(joined, "{#PROBE}: Traffic ({#IFACE}, {#DIRECTION})") {
		t.Errorf("the traffic chart is missing or unnamed by its macros: %v", names)
	}
	if !strings.Contains(joined, "{#PROBE}: Speed ({#IFACE})") {
		t.Errorf("the speed chart lost its series on Linux: %v", names)
	}
	if strings.Contains(joined, "Only Windows") {
		t.Errorf("a chart whose only metric the platform does not produce is still declared: %v", names)
	}
}

func TestAChartAcrossTwoRulesBecomesOneChartPerRule(t *testing.T) {
	def := graphDefinition()
	def.Graphs = []transformers.GraphDefinition{{Name: "Mixed", Series: []string{"rx", "speed"}}}
	exp, err := Generate(def, Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		for _, g := range r.GraphPrototypes {
			n++
			if len(g.GraphItems) != 1 {
				t.Errorf("%q draws %d items; the two live under different rules and cannot share a chart", g.Name, len(g.GraphItems))
			}
		}
	}
	if n != 2 {
		t.Errorf("charts = %d, want one per rule", n)
	}
}

func TestADashboardHoldsTheChartsOfItsTemplate(t *testing.T) {
	exp, err := Generate(graphDefinition(), Options{Platform: "windows", Version: "7.0"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	if len(tpl.Dashboards) != 1 {
		t.Fatalf("dashboards = %d, want 1", len(tpl.Dashboards))
	}
	graphs := 0
	for _, r := range tpl.DiscoveryRules {
		graphs += len(r.GraphPrototypes)
	}
	widgets := tpl.Dashboards[0].Pages[0].Widgets
	if len(widgets) != graphs {
		t.Errorf("%d widgets for %d graph prototypes", len(widgets), graphs)
	}
	refs := map[string]bool{}
	for _, w := range widgets {
		ref := w.Fields[2].Value.(string)
		if refs[ref] {
			t.Errorf("widget reference %s used twice", ref)
		}
		refs[ref] = true
		if w.Fields[1].Value.(widgetGraphRef).Host != tpl.Template {
			t.Error("a widget draws a graph of another template, which Zabbix refuses")
		}
	}
}

func TestTheSixPointZeroExportCarriesGraphsButNoDashboard(t *testing.T) {
	exp, err := Generate(graphDefinition(), Options{Platform: "linux", Version: "6.0"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	if len(tpl.Dashboards) != 0 {
		t.Error("the 6.0 dashboard widgets are written differently and are not generated")
	}
	graphs := 0
	for _, r := range tpl.DiscoveryRules {
		graphs += len(r.GraphPrototypes)
	}
	if graphs == 0 {
		t.Error("the graphs are the same in 6.0 and must be kept")
	}
}

// Every chart declared in a shipped definition must resolve on at least
// one platform, so a typo in a metric name does not silently drop a chart.
func TestEveryDeclaredChartResolvesSomewhere(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	for name, def := range defs {
		for _, g := range def.Graphs {
			known := map[string]bool{}
			for _, m := range def.Metrics {
				known[m.Name] = true
			}
			for _, s := range g.Series {
				if !known[s] {
					t.Errorf("%s: chart %q names %q, which is not a metric of the definition", name, g.Name, s)
				}
			}
			resolved := false
			for _, platform := range []string{"linux", "windows"} {
				for _, version := range []string{"6.0", "7.0"} {
					exp, err := Generate(def, Options{Platform: platform, Version: version})
					if err != nil {
						t.Fatalf("%s on %s: %v", name, platform, err)
					}
					for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
						for _, gp := range r.GraphPrototypes {
							if strings.Contains(gp.Name, ": "+g.Name) {
								resolved = true
								if !strings.Contains(gp.Name, "{#") {
									t.Errorf("%s: chart %q carries no discovery macro", name, gp.Name)
								}
							}
						}
					}
				}
			}
			if !resolved {
				t.Errorf("%s: chart %q has no series on any platform", name, g.Name)
			}
		}
	}
}
