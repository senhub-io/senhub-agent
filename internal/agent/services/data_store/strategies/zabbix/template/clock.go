package template

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// ClockMetric is the OTel name of the host clock. A clock is the one
// metric whose meaning is a comparison with the server's own time, so
// its template carries a trigger on that comparison instead of a
// threshold on the value.
const ClockMetric = "senhub.system.time"

// ClockDriftMacro holds how far the host clock may be from the server's
// before the trigger fires.
const ClockDriftMacro = "{$SENHUB.CLOCK.DRIFT.MAX}"

// clockTriggers hangs the drift trigger under the prototype of the host
// clock. fuzzytime() is the function Zabbix provides for exactly this
// check, and it takes the same arguments in 6.0 and 7.0. The value is
// the time of the last collection, so the comparison also includes the
// age of that sample (up to one collection interval), which the default
// leaves room for.
func clockTriggers(template string, def transformers.ProbeDefinition, opts Options,
	rules map[string]*DiscoveryRule) []Macro {
	var macros []Macro
	for _, m := range def.Metrics {
		if otelNameOf(m) != ClockMetric || (m.Otel != nil && m.Otel.Skip) {
			continue
		}
		labels := dimensions(def, m)
		rule := rules[discoveryKey(opts.Prefix, def.ProbeName, labels)]
		if rule == nil {
			continue
		}
		key := prototypeKey(opts.Prefix, def.ProbeName, m, labels)
		for i := range rule.ItemPrototypes {
			if rule.ItemPrototypes[i].Key != key {
				continue
			}
			t := TriggerPrototype{
				Expression: fmt.Sprintf("fuzzytime(/%s/%s,%s)=0", template, key, ClockDriftMacro),
				Name:       "{#PROBE}: host clock differs from the server's by more than " + ClockDriftMacro,
				Priority:   "WARNING",
				Description: "Raised when the host's time, as reported by the agent, is further from the " +
					"Zabbix server's time than the macro allows. Override the macro on a host or a host group to change it.",
				Tags: []Tag{{Tag: "scope", Value: "availability"}},
			}
			t.UUID = uid("trigger", template, t.Expression)
			rule.ItemPrototypes[i].TriggerPrototypes = append(rule.ItemPrototypes[i].TriggerPrototypes, t)
			macros = append(macros, Macro{
				Macro:       ClockDriftMacro,
				Value:       "60s",
				Description: "largest accepted difference between the host clock and the server's",
			})
		}
	}
	return macros
}
