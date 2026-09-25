package template

import (
	"regexp"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

type severityLookups struct {
	text     map[string]map[int]string
	severity map[string]map[int]string
}

func (s severityLookups) Lookup(id string) (map[int]string, bool) {
	m, ok := s.text[id]
	return m, ok
}

func (s severityLookups) Severities(id string) (map[int]string, bool) {
	m, ok := s.severity[id]
	return m, ok
}

func prototypesByKey(t *testing.T, exp Export) map[string]ItemPrototype {
	t.Helper()
	out := map[string]ItemPrototype{}
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		for _, p := range r.ItemPrototypes {
			out[p.Key] = p
		}
	}
	return out
}

// A state metric raises the problems its lookup names, with the same
// severities PRTG and Nagios read from it; an "ok" code raises nothing.
func TestAStateMetricRaisesTheProblemsItsLookupNames(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "netscaler", Metrics: []transformers.MetricDefinition{
		{Name: "vserver_state", DisplayName: "vServer State", Unit: "#", Lookup: "lb.state",
			Otel: &transformers.OtelMapping{Name: "netscaler.lbvserver.state", Unit: "1", Type: "gauge"}},
	}}
	lookups := severityLookups{
		text:     map[string]map[int]string{"lb.state": {1: "DOWN", 2: "UNKNOWN", 3: "BUSY", 7: "UP"}},
		severity: map[string]map[int]string{"lb.state": {1: "error", 2: "warning", 3: "warning", 7: "ok"}},
	}
	exp, err := Generate(def, Options{Lookups: lookups})
	if err != nil {
		t.Fatal(err)
	}
	p := prototypesByKey(t, exp)["senhub.netscaler.lbvserver.state[{#PROBE}]"]
	if len(p.TriggerPrototypes) != 2 {
		t.Fatalf("triggers = %d, want one for the error codes and one for the warnings", len(p.TriggerPrototypes))
	}
	high, warn := p.TriggerPrototypes[0], p.TriggerPrototypes[1]
	if high.Priority != "HIGH" || !strings.HasSuffix(high.Expression, "=1") || strings.Contains(high.Expression, "=7") {
		t.Errorf("error trigger = %s %q", high.Priority, high.Expression)
	}
	if warn.Priority != "WARNING" || !strings.Contains(warn.Expression, "=2 or last(") || !strings.HasSuffix(warn.Expression, "=3") {
		t.Errorf("warning trigger = %s %q", warn.Priority, warn.Expression)
	}
	if problems := Validate(exp); len(problems) > 0 {
		t.Errorf("the export would be refused: %v", problems)
	}
}

// A threshold becomes a pair of triggers held in macros a site tunes per
// host, the warning depending on the critical one.
func TestAThresholdBecomesTunableTriggers(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "memory", Metrics: []transformers.MetricDefinition{
		{Name: "memory_used_percent", DisplayName: "Memory Usage", Unit: "%",
			AlertThresholdWarning: 85, AlertThresholdCritical: 95,
			Otel: &transformers.OtelMapping{Name: "system.memory.utilization", Unit: "1", Type: "gauge"}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tpl := exp.ZabbixExport.Templates[0]
	macros := map[string]string{}
	for _, m := range tpl.Macros {
		macros[m.Macro] = m.Value
	}
	if macros["{$SENHUB.MEMORY_USED_PERCENT.WARN}"] != "85" || macros["{$SENHUB.MEMORY_USED_PERCENT.CRIT}"] != "95" {
		t.Errorf("macros = %v", macros)
	}
	p := prototypesByKey(t, exp)["senhub.system.memory.utilization[{#PROBE}]"]
	if len(p.TriggerPrototypes) != 2 {
		t.Fatalf("triggers = %d, want critical and warning", len(p.TriggerPrototypes))
	}
	crit, warn := p.TriggerPrototypes[0], p.TriggerPrototypes[1]
	if crit.Expression != "min(/SenHub memory/senhub.system.memory.utilization[{#PROBE}],5m)>{$SENHUB.MEMORY_USED_PERCENT.CRIT}" {
		t.Errorf("critical expression = %q", crit.Expression)
	}
	if len(warn.Dependencies) != 1 || warn.Dependencies[0].Expression != crit.Expression {
		t.Errorf("the warning must depend on the critical trigger; got %v", warn.Dependencies)
	}
}

// On a family prototype, a member's threshold must not fire on the rows
// of the other members: the free share of a drive is not held to the used
// threshold.
func TestAFamilyMemberThresholdStaysOnItsOwnRows(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "logicaldisk", Metrics: []transformers.MetricDefinition{
		{Name: "disk_used_percent", DisplayName: "Disk Used Percent ({drive})", Unit: "%", MultiInstanceLabels: []string{"drive"},
			AlertThresholdWarning: 80, AlertThresholdCritical: 90,
			Otel: &transformers.OtelMapping{Name: "system.filesystem.utilization", Unit: "1", Type: "gauge", Attributes: map[string]string{"state": "used"}}},
		{Name: "disk_free_percent", DisplayName: "Disk Free Percent ({drive})", Unit: "%", MultiInstanceLabels: []string{"drive"},
			Otel: &transformers.OtelMapping{Name: "system.filesystem.utilization", Unit: "1", Type: "gauge", Attributes: map[string]string{"state": "free"}}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var overrides []Override
	for _, r := range exp.ZabbixExport.Templates[0].DiscoveryRules {
		overrides = append(overrides, r.Overrides...)
	}
	if len(overrides) != 2 {
		t.Fatalf("overrides = %d, want one per trigger of the member", len(overrides))
	}
	c := overrides[0].Filter.Conditions[0]
	if c.Macro != "{#STATE}" || c.Value != "^used$" || c.Operator != "NOT_MATCHES_REGEX" {
		t.Errorf("condition = %+v", c)
	}
	if op := overrides[0].Operations[0]; op.OperationObject != "TRIGGER_PROTOTYPE" || op.Discover != "NO_DISCOVER" {
		t.Errorf("operation = %+v", op)
	}
	if problems := Validate(exp); len(problems) > 0 {
		t.Errorf("the export would be refused: %v", problems)
	}
}

// A threshold written in a unit the server does not show would never
// fire; the generator refuses it rather than ship a silent trigger.
func TestAThresholdInAnotherUnitThanShownIsRefused(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "memory", Metrics: []transformers.MetricDefinition{
		{Name: "memory_free_mb", DisplayName: "Free", Unit: "MB", AlertThresholdCritical: 500,
			Otel: &transformers.OtelMapping{Name: "system.memory.usage", Unit: "By", Type: "gauge"}},
	}}
	if _, err := Generate(def, Options{}); err == nil {
		t.Fatal("a threshold in MB on an item shown in B was accepted")
	}
}

// Items and triggers carry the tags the native templates use, which is
// what problem views, dashboards and actions filter on.
func TestItemsAndTriggersCarryTheNativeTags(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "cpu", Metrics: []transformers.MetricDefinition{
		{Name: "cpu_usage_total", DisplayName: "CPU Total Usage", Unit: "%",
			AlertThresholdWarning: 80, AlertThresholdCritical: 90,
			Otel: &transformers.OtelMapping{Name: "system.cpu.utilization", Unit: "1", Type: "gauge"}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := prototypesByKey(t, exp)["senhub.system.cpu.utilization[{#PROBE}]"]
	if len(p.Tags) != 1 || p.Tags[0] != (Tag{Tag: "component", Value: "cpu"}) {
		t.Errorf("item tags = %v, want component: cpu", p.Tags)
	}
	for _, tr := range p.TriggerPrototypes {
		if len(tr.Tags) != 1 || tr.Tags[0] != (Tag{Tag: "scope", Value: "performance"}) {
			t.Errorf("trigger %q tags = %v, want scope: performance", tr.Name, tr.Tags)
		}
	}
	for _, it := range Base(Options{}).ZabbixExport.Templates[0].Items {
		if len(it.Tags) != 1 || it.Tags[0].Tag != "component" {
			t.Errorf("agent item %s has tags %v", it.Key, it.Tags)
		}
	}
}

// A rule holding several prototypes gets one override per prototype,
// acting only when the agent sent the list and the list lacks the metric.
func TestEachPrototypeIsHeldToTheFedList(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "network", Metrics: []transformers.MetricDefinition{
		{Name: "interface_speed", DisplayName: "{interface} Speed", MultiInstanceLabels: []string{"interface"},
			Otel: &transformers.OtelMapping{Name: "senhub.system.network.interface.speed", Unit: "bit/s", Type: "gauge"}},
		{Name: "interface_up", DisplayName: "{interface} Up", MultiInstanceLabels: []string{"interface"},
			Otel: &transformers.OtelMapping{Name: "senhub.system.network.interface.up", Unit: "1", Type: "gauge"}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rule := exp.ZabbixExport.Templates[0].DiscoveryRules[0]
	if len(rule.Overrides) != 2 {
		t.Fatalf("overrides = %d, want one per prototype", len(rule.Overrides))
	}
	o := rule.Overrides[0]
	if o.Filter.EvalType != "AND" || o.Filter.Conditions[0].Operator != "EXISTS" ||
		o.Filter.Conditions[1].Value != `,senhub\.system\.network\.interface\.speed,` {
		t.Errorf("override = %+v", o.Filter)
	}
	if op := o.Operations[0]; op.OperationObject != "ITEM_PROTOTYPE" || op.Operator != "REGEXP" ||
		!regexp.MustCompile(op.Value).MatchString("network: eth0 Speed") || regexp.MustCompile(op.Value).MatchString("network: eth0 Up") {
		t.Errorf("operation = %+v", op)
	}
	if problems := Validate(exp); len(problems) > 0 {
		t.Errorf("the export would be refused: %v", problems)
	}
}
