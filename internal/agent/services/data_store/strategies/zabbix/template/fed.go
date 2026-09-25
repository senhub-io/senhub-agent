package template

import (
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// FedMacro is the discovery macro in which the agent lists, for one
// instance, the metrics that instance actually sends.
//
// A discovery rule is keyed on a probe type and a set of dimensions, and
// every prototype under it becomes an item for every discovered instance.
// An instance that never produces one of those metrics got an item that
// stayed empty for ever: the speed of a virtio NIC, whose kernel reports
// none, beside a bridge on the same host that does. The template holds
// each prototype to this list through an LLD override, so the item is
// only created where its metric is fed.
const FedMacro = "{#SENHUB.FED}"

// FedID names a metric in FedMacro: its OTel name and the values of the
// attributes that set it apart from the other metrics sharing that name.
// The agent and the generator both call it, so the two cannot drift.
func FedID(m transformers.MetricDefinition) string {
	parts := append([]string{otelNameOf(m)}, attributeValues(m)...)
	for i, p := range parts {
		parts[i] = sanitizeKeyName(p)
	}
	return strings.Join(parts, ":")
}

// FedList renders the ids as the macro's value, delimited on both ends so
// a match on ",id," never mistakes one id for the prefix of another.
func FedList(ids []string) string {
	return "," + strings.Join(ids, ",") + ","
}

// ambiguous reports a prototype whose name pattern also matches another
// prototype of the rule once its macros hold an ordinary value. Such an
// override could switch off an item that is fed, which is worse than the
// empty item it removes, so the prototype gets none.
func ambiguous(rule *DiscoveryRule, i int) bool {
	re := regexp.MustCompile(nameRegexp(rule.ItemPrototypes[i].Name))
	for j, p := range rule.ItemPrototypes {
		if j != i && re.MatchString(lldMacro.ReplaceAllString(p.Name, "x")) {
			return true
		}
	}
	return false
}

// fedOverrides adds to a rule one override per prototype, which keeps the
// rule from creating that item on a row whose FedMacro does not list its
// metric. The macro must exist for the override to act, so an agent too
// old to send it keeps every item as before.
func fedOverrides(rule *DiscoveryRule, fedOf map[string]string) {
	if len(rule.ItemPrototypes) < 2 {
		return
	}
	named := map[string]int{}
	for i, p := range rule.ItemPrototypes {
		id, ok := fedOf[p.Key]
		if !ok || ambiguous(rule, i) {
			continue
		}
		// A histogram's count and sum share one id; an override name must
		// be unique within its rule.
		name := "Not fed: " + id
		if named[id]++; named[id] > 1 {
			name += " (" + itoa(named[id]) + ")"
		}
		rule.Overrides = append(rule.Overrides, Override{
			Name: name,
			Step: itoa(len(rule.Overrides) + 1),
			Filter: OverrideFilter{EvalType: "AND", Conditions: []OverrideCondition{
				{Macro: FedMacro, Operator: "EXISTS", FormulaID: "A"},
				{Macro: FedMacro, Value: "," + regexpQuote(id) + ",", Operator: "NOT_MATCHES_REGEX", FormulaID: "B"},
			}},
			Operations: []OverrideOperation{{OperationObject: "ITEM_PROTOTYPE", Operator: "REGEXP", Value: nameRegexp(p.Name), Discover: "NO_DISCOVER"}},
		})
	}
}
