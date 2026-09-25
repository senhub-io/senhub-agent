package template

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// TriggerPrototype is a trigger created for every discovered instance of
// the item prototype it hangs under.
type TriggerPrototype struct {
	UUID         string       `yaml:"uuid"`
	Expression   string       `yaml:"expression"`
	Name         string       `yaml:"name"`
	Priority     string       `yaml:"priority"`
	Description  string       `yaml:"description,omitempty"`
	Dependencies []TriggerRef `yaml:"dependencies,omitempty"`
	Tags         []Tag        `yaml:"tags,omitempty"`
}

// TriggerRef names another trigger the way an export does: by its name
// and its expression together.
type TriggerRef struct {
	Name       string `yaml:"name"`
	Expression string `yaml:"expression"`
}

// Macro is a template user macro; a host or a host group overrides it.
type Macro struct {
	Macro       string `yaml:"macro"`
	Value       string `yaml:"value"`
	Description string `yaml:"description,omitempty"`
}

// Override changes what a discovery rule creates for the rows its filter
// matches.
type Override struct {
	Name       string              `yaml:"name"`
	Step       string              `yaml:"step"`
	Filter     OverrideFilter      `yaml:"filter"`
	Operations []OverrideOperation `yaml:"operations"`
}

type OverrideFilter struct {
	EvalType   string              `yaml:"evaltype"`
	Conditions []OverrideCondition `yaml:"conditions"`
}

type OverrideCondition struct {
	Macro     string `yaml:"macro"`
	Value     string `yaml:"value"`
	Operator  string `yaml:"operator"`
	FormulaID string `yaml:"formulaid"`
}

type OverrideOperation struct {
	OperationObject string `yaml:"operationobject"`
	Operator        string `yaml:"operator"`
	Value           string `yaml:"value"`
	Discover        string `yaml:"discover"`
}

// sustained is how long a threshold must hold before its trigger fires,
// which keeps a one-poll spike from paging anyone.
const sustained = "5m"

// stateTriggers turns a state metric into one trigger per severity: the
// codes its lookup calls "error" raise a high-priority problem, the ones
// it calls "warning" a warning. The lookup is the same one PRTG and
// Nagios read their state from, so the three outputs agree on what is a
// problem.
func stateTriggers(template string, proto ItemPrototype, bySeverity map[int]string) []TriggerPrototype {
	var out []TriggerPrototype
	for _, level := range []struct{ severity, priority, word string }{
		{"error", "HIGH", "an error state"},
		{"warning", "WARNING", "a warning state"},
	} {
		var codes []int
		for code, sev := range bySeverity {
			if sev == level.severity {
				codes = append(codes, code)
			}
		}
		if len(codes) == 0 {
			continue
		}
		sort.Ints(codes)
		terms := make([]string, len(codes))
		for i, c := range codes {
			terms[i] = fmt.Sprintf("last(/%s/%s)=%d", template, proto.Key, c)
		}
		t := TriggerPrototype{
			Expression:  strings.Join(terms, " or "),
			Name:        proto.Name + " is in " + level.word + ": {ITEM.LASTVALUE1}",
			Priority:    level.priority,
			Description: "Raised while the value is one the metric's lookup classes as " + level.severity + ".",
			Tags:        []Tag{{Tag: "scope", Value: "availability"}},
		}
		t.UUID = uid("trigger", template, t.Expression)
		out = append(out, t)
	}
	return out
}

// thresholdMacro names the user macro holding one threshold of a metric.
func thresholdMacro(m transformers.MetricDefinition, level string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(m.Name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return "{$SENHUB." + b.String() + "." + level + "}"
}

// thresholdTriggers turns the `alert_threshold_warning` and
// `alert_threshold_critical` of a definition into triggers on the
// matching prototypes, each threshold held in a user macro so a site
// tunes it per host or per group without editing the template. The
// warning depends on the critical one, so a value past both raises one
// problem, not two.
//
// A threshold is written in the definition's unit, which must be the unit
// the server shows: a threshold of 90 on a fraction would never fire.
func thresholdTriggers(template string, def transformers.ProbeDefinition, opts Options,
	rules map[string]*DiscoveryRule, familyOf map[string]*family) ([]Macro, error) {
	var macros []Macro
	for _, m := range def.Metrics {
		if m.AlertThresholdWarning == 0 && m.AlertThresholdCritical == 0 {
			continue
		}
		if m.Otel != nil && m.Otel.Skip {
			continue
		}
		shown := units(m)
		if isRatioOfWhole(m) {
			shown = "%"
		}
		if shown != m.Unit {
			return nil, fmt.Errorf("%s.%s: a threshold is written in %q but Zabbix shows %q", def.ProbeName, m.Name, m.Unit, shown)
		}

		labels := dimensions(def, m)
		ruleKey := discoveryKey(opts.Prefix, def.ProbeName, labels)
		key := prototypeKey(opts.Prefix, m, labels)
		f := familyOf[m.Name]
		if f != nil {
			ruleKey = variantRuleKey(opts.Prefix, def.ProbeName, f.otelName, labels)
			key = variantPrototypeKey(opts.Prefix, f)
		}
		rule := rules[ruleKey]
		if rule == nil {
			continue
		}
		var proto *ItemPrototype
		for i := range rule.ItemPrototypes {
			if rule.ItemPrototypes[i].Key == key {
				proto = &rule.ItemPrototypes[i]
			}
		}
		if proto == nil {
			continue
		}

		subject := prototypeName(m, labels)
		var made []TriggerPrototype
		for _, level := range []struct {
			name, priority string
			value          int
		}{
			{"CRIT", "HIGH", m.AlertThresholdCritical},
			{"WARN", "WARNING", m.AlertThresholdWarning},
		} {
			if level.value == 0 {
				continue
			}
			macro := thresholdMacro(m, level.name)
			macros = append(macros, Macro{
				Macro:       macro,
				Value:       strconv.Itoa(level.value),
				Description: fmt.Sprintf("%s threshold of %s, in %s", strings.ToLower(level.priority), firstNonEmpty(m.DisplayName, m.Name), m.Unit),
			})
			t := TriggerPrototype{
				Expression: fmt.Sprintf("min(/%s/%s,%s)>%s", template, proto.Key, sustained, macro),
				Name:       fmt.Sprintf("%s is above %s%s", subject, macro, m.Unit),
				Priority:   level.priority,
				Tags:       []Tag{{Tag: "scope", Value: thresholdScope(m)}},
				Description: fmt.Sprintf("Raised when every value over the last %s is above %s. "+
					"Override the macro on a host or a host group to change it.", sustained, macro),
			}
			t.UUID = uid("trigger", template, t.Expression)
			made = append(made, t)
		}
		if len(made) == 2 {
			made[1].Dependencies = []TriggerRef{{Name: made[0].Name, Expression: made[0].Expression}}
		}
		proto.TriggerPrototypes = append(proto.TriggerPrototypes, made...)

		// A family's prototype stands for every member, so the rule must
		// not create this member's triggers on the rows of the others:
		// the free space of a drive is not held to its used threshold.
		if f != nil {
			attrs := attrMacros(f.labels, f.attrKeys)
			var conds []OverrideCondition
			for i, k := range f.attrKeys {
				conds = append(conds, OverrideCondition{
					Macro:     attrs[i],
					Value:     "^" + regexpQuote(m.Otel.Attributes[k]) + "$",
					Operator:  "NOT_MATCHES_REGEX",
					FormulaID: string(rune('A' + i)),
				})
			}
			for _, t := range made {
				rule.Overrides = append(rule.Overrides, Override{
					Name:       "Only on " + m.Name + ": " + t.Priority,
					Step:       strconv.Itoa(len(rule.Overrides) + 1),
					Filter:     OverrideFilter{EvalType: "OR", Conditions: conds},
					Operations: []OverrideOperation{{OperationObject: "TRIGGER_PROTOTYPE", Operator: "REGEXP", Value: nameRegexp(t.Name), Discover: "NO_DISCOVER"}},
				})
			}
		}
	}
	return macros, nil
}

// thresholdScope classes a threshold the way the native templates do: a
// processor running hot is a performance problem, a memory or a disk
// filling up is a capacity one.
func thresholdScope(m transformers.MetricDefinition) string {
	if strings.HasPrefix(otelNameOf(m), "system.cpu.") {
		return "performance"
	}
	return "capacity"
}

func itoa(n int) string { return strconv.Itoa(n) }

func regexpQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
