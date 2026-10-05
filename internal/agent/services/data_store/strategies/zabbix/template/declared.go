package template

import (
	"fmt"
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Triggers and discovery filters declared in a definition (`triggers:`,
// `discovery_filters:`). They are the generic form of what a definition
// cannot say with one threshold pair on one metric: a condition over
// several metrics of an instance (an automatic service that is not
// running), or a threshold relative to another metric (a queue per core).

var metricRef = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// declaredTriggers attaches the declared triggers to the item prototype of
// the first metric they mention, under the rule that metric lives in, and
// returns the user macros they ship. A trigger naming a metric the
// platform does not produce is left out; one mixing two discovery rules
// is an error in the definition, because Zabbix refuses a trigger
// prototype over items of two rules.
func declaredTriggers(template string, def transformers.ProbeDefinition, placed map[string]placement, rules map[string]*DiscoveryRule) ([]Macro, error) {
	var macros []Macro
	seenMacro := map[string]bool{}
	for _, d := range def.Triggers {
		refs := metricRef.FindAllStringSubmatch(d.Expression, -1)
		if len(refs) == 0 {
			return nil, fmt.Errorf("%s: trigger %q names no metric, write one as {{metric_name}}", def.ProbeName, d.Name)
		}
		var first placement
		available := true
		expression := d.Expression
		for i, r := range refs {
			// {{a|b}} takes the first metric the platform produces,
			// for a fact one platform calls by another name.
			var pl placement
			ok := false
			for _, alt := range strings.Split(r[1], "|") {
				if pl, ok = placed[strings.TrimSpace(alt)]; ok {
					break
				}
			}
			if !ok {
				available = false
				break
			}
			if i == 0 {
				first = pl
			} else if pl.ruleKey != first.ruleKey {
				return nil, fmt.Errorf("%s: trigger %q mixes the discovery rules %s and %s", def.ProbeName, d.Name, first.ruleKey, pl.ruleKey)
			}
			expression = strings.ReplaceAll(expression, r[0], "/"+template+"/"+pl.key)
		}
		if !available {
			continue
		}
		rule := rules[first.ruleKey]
		var proto *ItemPrototype
		for i := range rule.ItemPrototypes {
			if rule.ItemPrototypes[i].Key == first.key {
				proto = &rule.ItemPrototypes[i]
			}
		}
		if proto == nil {
			continue
		}

		name := d.Name
		if !strings.Contains(name, "{#") {
			name = probeMacroName + ": " + name
			if len(first.macros) > 0 {
				name += " (" + strings.Join(first.macros, ", ") + ")"
			}
		}
		priority := d.Priority
		if priority == "" {
			priority = "WARNING"
		}
		t := TriggerPrototype{
			Expression:  expression,
			Name:        name,
			Priority:    priority,
			Description: d.Description,
			Tags:        []Tag{{Tag: "scope", Value: firstNonEmpty(d.Scope, "availability")}},
		}
		t.UUID = uid("trigger", template, t.Expression)
		proto.TriggerPrototypes = append(proto.TriggerPrototypes, t)

		for _, m := range d.Macros {
			if seenMacro[m.Name] {
				continue
			}
			seenMacro[m.Name] = true
			macros = append(macros, Macro{Macro: m.Name, Value: m.Value, Description: m.Description})
		}
	}
	return macros, nil
}

// discoveryFilters holds the rules that discover a dimension to the
// patterns the definition names, and returns the macros carrying them.
func discoveryFilters(def transformers.ProbeDefinition, placed map[string]placement, rules map[string]*DiscoveryRule) []Macro {
	var macros []Macro
	for _, f := range def.DiscoveryFilters {
		target := macroFor(f.Label)
		applied := false
		done := map[string]bool{}
		for _, pl := range placed {
			if done[pl.ruleKey] {
				continue
			}
			has := false
			for _, m := range pl.macros {
				if m == target {
					has = true
				}
			}
			if !has {
				continue
			}
			done[pl.ruleKey] = true
			applied = true
			rule := rules[pl.ruleKey]
			if rule.Filter == nil {
				rule.Filter = &RuleFilter{EvalType: "AND"}
			}
			n := len(rule.Filter.Conditions)
			rule.Filter.Conditions = append(rule.Filter.Conditions,
				FilterCondition{Macro: target, Value: f.MatchMacro, FormulaID: string(rune('A' + n))},
				FilterCondition{Macro: target, Value: f.ExcludeMacro, Operator: "NOT_MATCHES_REGEX", FormulaID: string(rune('A' + n + 1))},
			)
		}
		if applied {
			macros = append(macros,
				Macro{Macro: f.MatchMacro, Value: ".*", Description: "Instances of " + f.Label + " to discover: a regular expression the value must match. " + f.Description},
				Macro{Macro: f.ExcludeMacro, Value: f.ExcludeDefault, Description: "Instances of " + f.Label + " left out of discovery: a regular expression the value must not match. " + f.Description},
			)
		}
	}
	return macros
}
