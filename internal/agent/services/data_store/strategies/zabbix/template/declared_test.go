package template

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func declaredDefinition() transformers.ProbeDefinition {
	gauge := func(name string) *transformers.OtelMapping {
		return &transformers.OtelMapping{Name: name, Unit: "1", Type: "gauge"}
	}
	return transformers.ProbeDefinition{
		ProbeName:    "svc",
		FriendlyName: "Svc",
		Metrics: []transformers.MetricDefinition{
			{Name: "state", DisplayName: "Svc {name} State", MultiInstanceLabels: []string{"name"}, Otel: gauge("svc.state")},
			{Name: "start", DisplayName: "Svc {name} Start", MultiInstanceLabels: []string{"name"}, Otel: gauge("svc.start")},
			{Name: "winonly", DisplayName: "Svc {name} Win", MultiInstanceLabels: []string{"name"}, Platforms: []string{"windows"}, Otel: gauge("svc.win")},
			{Name: "alone", DisplayName: "Svc Alone", Otel: gauge("svc.alone")},
		},
		Triggers: []transformers.TriggerDefinition{
			{
				Name:       "Svc {#NAME} is down",
				Expression: "max({{state}},{$SENHUB.SVC.GRACE})=0 and last({{start}})=2",
				Priority:   "AVERAGE", Scope: "availability",
				Macros: []transformers.MacroDefinition{{Name: "{$SENHUB.SVC.GRACE}", Value: "5m", Description: "grace"}},
			},
			{Name: "Windows only", Expression: "last({{winonly}})=1"},
		},
		DiscoveryFilters: []transformers.DiscoveryFilter{
			{Label: "name", MatchMacro: "{$SENHUB.SVC.MATCHES}", ExcludeMacro: "{$SENHUB.SVC.NOT_MATCHES}", ExcludeDefault: "^skip$"},
		},
	}
}

func findTrigger(tpl Template, name string) (TriggerPrototype, bool) {
	for _, r := range tpl.DiscoveryRules {
		for _, p := range r.ItemPrototypes {
			for _, tp := range p.TriggerPrototypes {
				if strings.Contains(tp.Name, name) {
					return tp, true
				}
			}
		}
	}
	return TriggerPrototype{}, false
}

func TestADeclaredTriggerReadsTheItemsOfItsInstance(t *testing.T) {
	exp, err := Generate(declaredDefinition(), Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	tp, ok := findTrigger(tpl, "is down")
	if !ok {
		t.Fatal("the declared trigger is missing")
	}
	for _, want := range []string{
		"max(/" + tpl.Template + "/senhub.svc.state[{#PROBE_SVC},{#NAME}],{$SENHUB.SVC.GRACE})=0",
		"last(/" + tpl.Template + "/senhub.svc.start[{#PROBE_SVC},{#NAME}])=2",
	} {
		if !strings.Contains(tp.Expression, want) {
			t.Errorf("expression %q lacks %q", tp.Expression, want)
		}
	}
	if strings.Contains(tp.Expression, "{{") {
		t.Errorf("a metric reference was left unresolved: %s", tp.Expression)
	}
	if tp.Priority != "AVERAGE" {
		t.Errorf("priority = %s", tp.Priority)
	}
	found := false
	for _, m := range tpl.Macros {
		if m.Macro == "{$SENHUB.SVC.GRACE}" && m.Value == "5m" {
			found = true
		}
	}
	if !found {
		t.Error("the macro the trigger declares is not shipped with the template")
	}
}

func TestADeclaredTriggerOverAMetricThePlatformLacksIsLeftOut(t *testing.T) {
	linux, err := Generate(declaredDefinition(), Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findTrigger(linux.ZabbixExport.Templates[0], "Windows only"); ok {
		t.Error("a trigger over a Windows-only metric is declared on Linux")
	}
	win, err := Generate(declaredDefinition(), Options{Platform: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findTrigger(win.ZabbixExport.Templates[0], "Windows only"); !ok {
		t.Error("the Windows trigger is missing on Windows")
	}
}

func TestADeclaredTriggerAcrossTwoRulesIsAnError(t *testing.T) {
	def := declaredDefinition()
	def.Triggers = []transformers.TriggerDefinition{{Name: "Mixed", Expression: "last({{state}})=0 and last({{alone}})=0"}}
	if _, err := Generate(def, Options{}); err == nil {
		t.Fatal("a trigger prototype over items of two rules is refused by Zabbix and must be refused here")
	}
}

func TestADiscoveryFilterHoldsTheRuleToTheMacros(t *testing.T) {
	exp, err := Generate(declaredDefinition(), Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	filtered := 0
	for _, r := range tpl.DiscoveryRules {
		if r.Filter == nil {
			continue
		}
		filtered++
		// Two conditions on one macro are ORed under AND_OR, which would
		// discover everything; they must be ANDed.
		if r.Filter.EvalType != "AND" || len(r.Filter.Conditions) != 2 {
			t.Errorf("filter = %+v", r.Filter)
		}
		if r.Filter.Conditions[1].Operator != "NOT_MATCHES_REGEX" {
			t.Errorf("the exclusion must be a negative match: %+v", r.Filter.Conditions[1])
		}
	}
	if filtered != 1 {
		t.Errorf("filtered rules = %d; only the rule that discovers the dimension is held to it", filtered)
	}
	have := map[string]string{}
	for _, m := range tpl.Macros {
		have[m.Macro] = m.Value
	}
	if have["{$SENHUB.SVC.MATCHES}"] != ".*" || have["{$SENHUB.SVC.NOT_MATCHES}"] != "^skip$" {
		t.Errorf("filter macros = %v", have)
	}
}

// The Windows services template is the reason these exist: a service set
// to start automatically that is not running raises a problem, on the
// per-service rule, and not on Linux where there are no services.
func TestTheWindowsServicesTemplateRaisesAnAutomaticServiceNotRunning(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	win, err := Generate(defs["winservices"], Options{Platform: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	tp, ok := findTrigger(win.ZabbixExport.Templates[0], "start automatically and is not running")
	if !ok {
		t.Fatal("no trigger for an automatic service that is not running")
	}
	for _, want := range []string{"service.state[", "service.start_type[", "=2", "=5", "{#WINDOWS_SERVICE_NAME}"} {
		if !strings.Contains(tp.Expression, want) {
			t.Errorf("expression %q lacks %q", tp.Expression, want)
		}
	}
	linux, err := Generate(defs["winservices"], Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if !linux.DeclaresNothing() {
		t.Error("the services template must be empty on Linux")
	}
}
