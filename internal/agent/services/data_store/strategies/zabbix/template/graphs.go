package template

import (
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Graphs are declared in the definitions (`graphs:`), by metric name, so
// that a chart is written once and each platform draws the part of it
// its host produces: "Disk I/O" lists the Linux and the Windows metrics
// and a host gets the ones it feeds. Every chart is a graph prototype
// under the discovery rule of its metrics, named with the macros of that
// rule so each discovered instance gets its own.

// placement says where the item prototype of a metric was declared.
type placement struct {
	ruleKey string
	key     string
	macros  []string
}

func labelMacros(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = macroFor(l)
	}
	return out
}

type GraphPrototype struct {
	UUID       string      `yaml:"uuid"`
	Name       string      `yaml:"name"`
	GraphItems []GraphItem `yaml:"graph_items"`
}

type GraphItem struct {
	SortOrder string       `yaml:"sortorder,omitempty"`
	Color     string       `yaml:"color"`
	Item      GraphItemRef `yaml:"item"`
}

type GraphItemRef struct {
	Host string `yaml:"host"`
	Key  string `yaml:"key"`
}

// graphColors is the order series take their colour in, the one Zabbix's
// own templates start with.
var graphColors = []string{"199C0D", "F63100", "2774A4", "F7941D", "FC6EA3", "6C59DC", "AC8C14", "611F27"}

// graphPrototypes attaches the declared charts to the rules of their
// metrics. A metric the platform does not produce is left out of its
// chart, and a chart left with no series is not written. Zabbix draws a
// chart over the items of one rule only, and a definition can list the
// metrics of both platforms, which live under different rules; those get
// one chart per rule, each named with its own macros.
func graphPrototypes(template string, def transformers.ProbeDefinition, placed map[string]placement, rules map[string]*DiscoveryRule) {
	for _, g := range def.Graphs {
		var order []string
		byRule := map[string][]placement{}
		seen := map[string]bool{}
		for _, metric := range g.Series {
			pl, ok := placed[metric]
			if !ok || seen[pl.key] {
				continue
			}
			seen[pl.key] = true
			if _, ok := byRule[pl.ruleKey]; !ok {
				order = append(order, pl.ruleKey)
			}
			byRule[pl.ruleKey] = append(byRule[pl.ruleKey], pl)
		}
		for _, ruleKey := range order {
			series := byRule[ruleKey]
			name := probeMacroName + ": " + g.Name
			if len(series[0].macros) > 0 {
				name += " (" + strings.Join(series[0].macros, ", ") + ")"
			}
			gp := GraphPrototype{UUID: uid("graph", template, name), Name: name}
			for i, pl := range series {
				gi := GraphItem{Color: graphColors[i%len(graphColors)], Item: GraphItemRef{Host: template, Key: pl.key}}
				if i > 0 {
					gi.SortOrder = strconv.Itoa(i)
				}
				gp.GraphItems = append(gp.GraphItems, gi)
			}
			rules[ruleKey].GraphPrototypes = append(rules[ruleKey].GraphPrototypes, gp)
		}
	}
}

// Dashboards: see dashboardOf.

// Dashboard is a template dashboard of the 7.0 export format, whose
// widgets sit on a 72-column grid.
type Dashboard struct {
	UUID  string          `yaml:"uuid"`
	Name  string          `yaml:"name"`
	Pages []DashboardPage `yaml:"pages"`
}

type DashboardPage struct {
	Name    string   `yaml:"name,omitempty"`
	Widgets []Widget `yaml:"widgets"`
}

type Widget struct {
	Type   string        `yaml:"type"`
	X      string        `yaml:"x,omitempty"`
	Y      string        `yaml:"y,omitempty"`
	Width  string        `yaml:"width"`
	Height string        `yaml:"height"`
	Fields []WidgetField `yaml:"fields"`
}

type WidgetField struct {
	Type  string      `yaml:"type"`
	Name  string      `yaml:"name"`
	Value interface{} `yaml:"value"`
}

type widgetGraphRef struct {
	Host string `yaml:"host"`
	Name string `yaml:"name"`
}

// dashboardOf builds the dashboard of a template from its graph
// prototypes, two a row. Zabbix resolves a dashboard widget against the
// graphs of the template that holds the dashboard and of no other, even
// one it is linked to, so each probe template carries the dashboard of
// its own charts and a host shows them as tabs of its Dashboards page.
// The 6.0 export writes widgets differently and gets none.
func dashboardOf(tpl Template, title string, opts Options) []Dashboard {
	if opts.Version == "6.0" {
		return nil
	}
	var widgets []Widget
	for _, rule := range tpl.DiscoveryRules {
		for _, g := range rule.GraphPrototypes {
			i := len(widgets)
			widgets = append(widgets, Widget{
				Type:   "graphprototype",
				X:      xOf(i),
				Y:      yOf(i),
				Width:  "36",
				Height: "5",
				Fields: []WidgetField{
					{Type: "INTEGER", Name: "columns", Value: "1"},
					{Type: "GRAPH_PROTOTYPE", Name: "graphid.0", Value: widgetGraphRef{Host: tpl.Template, Name: g.Name}},
					{Type: "STRING", Name: "reference", Value: widgetReference(i)},
				},
			})
		}
	}
	if len(widgets) == 0 {
		return nil
	}
	return []Dashboard{{
		UUID:  uid("dashboard", tpl.Template),
		Name:  title,
		Pages: []DashboardPage{{Widgets: widgets}},
	}}
}

// Two widgets a row, left then right; a lone one on the last row stays
// on the left.
func xOf(i int) string {
	if i%2 == 0 {
		return ""
	}
	return "36"
}

func yOf(i int) string {
	if i/2 == 0 {
		return ""
	}
	return strconv.Itoa(5 * (i / 2))
}

// widgetReference is the five-letter key Zabbix wants on a dashboard
// widget, unique within the dashboard.
func widgetReference(i int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return "SH" + string([]byte{letters[(i/26)%26], letters[i%26]}) + "A"
}
