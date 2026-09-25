package zabbix

import (
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// A variant family is the set of metrics of one probe that share an OTel
// name and a dimension set and differ only by the values of the same
// attributes: the used and the free bytes of a filesystem, the bytes
// sent and received on an interface, the modes of a processor.
//
// The generator declares one prototype for the whole family, keyed on a
// macro rather than on each declared value, because it cannot know which
// values a given host feeds and declaring them all leaves a third of a
// host's items empty forever. This side is the other half of that
// bargain: the agent discovers, per instance, only the values it really
// sends, so only those items are created.
//
// It mirrors the generator's `template` package on purpose, the way the
// key builders already do; `TestGeneratedPrototypesNameTheKeysTheAgentSends`
// pins the two together on the real definitions.
type variantFamily struct {
	otelName string
	labels   []string
	attrKeys []string
}

// familiesOf returns, per metric name, the family it belongs to.
func familiesOf(def *transformers.ProbeDefinition) map[string]*variantFamily {
	if def == nil {
		return nil
	}
	type group struct {
		fam     *variantFamily
		members []transformers.MetricDefinition
	}
	grouped := map[string]*group{}
	var order []string
	for i := range def.Metrics {
		m := def.Metrics[i]
		if m.Otel != nil && m.Otel.Skip {
			continue
		}
		if len(attributeKeysOf(&m)) == 0 {
			continue
		}
		labels := dimensions(def, &m)
		id := otelNameOf(&m) + "\x00" + strings.Join(labels, ",")
		g, ok := grouped[id]
		if !ok {
			g = &group{fam: &variantFamily{otelName: otelNameOf(&m), labels: labels}}
			grouped[id] = g
			order = append(order, id)
		}
		g.members = append(g.members, m)
	}

	// A definition that asks for it gets one rule per metric: its metrics
	// are independent, so grouping them by the dimensions they share
	// would declare a prototype per metric under one rule, and whatever
	// the host does not feed would stay an empty item for ever (#922).
	perMetricRules := def.DiscoverPerMetric

	out := map[string]*variantFamily{}
	for _, id := range order {
		g := grouped[id]
		if len(g.members) < 2 && !perMetricRules {
			continue
		}
		if perMetricRules && len(g.members) < 2 {
			out[g.members[0].Name] = g.fam
			continue
		}
		keys := attributeKeysOf(&g.members[0])
		sameShape := true
		values := map[string]bool{}
		for i := range g.members {
			if !sameStrings(attributeKeysOf(&g.members[i]), keys) {
				sameShape = false
				break
			}
			values[strings.Join(staticAttributeValues(&g.members[i]), "\x00")] = true
		}
		if !sameShape || len(values) < 2 {
			continue
		}
		g.fam.attrKeys = keys
		for i := range g.members {
			out[g.members[i].Name] = g.fam
		}
	}
	return out
}

func otelNameOf(m *transformers.MetricDefinition) string {
	if m != nil && m.Otel != nil && m.Otel.Name != "" {
		return m.Otel.Name
	}
	if m == nil {
		return ""
	}
	return m.Name
}

func attributeKeysOf(m *transformers.MetricDefinition) []string {
	if m == nil || m.Otel == nil || len(m.Otel.Attributes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Otel.Attributes))
	for k := range m.Otel.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// attrMacros names the macro carrying each attribute; it must agree with
// the generator's, since the server substitutes one into the other.
func attrMacros(labels, attrKeys []string) []string {
	taken := map[string]bool{probeMacro: true}
	for _, l := range labels {
		taken[macroFor(l)] = true
	}
	short := make([]string, len(attrKeys))
	seen := map[string]int{}
	for i, k := range attrKeys {
		s := macroFor(lastSegment(k))
		short[i] = s
		seen[s]++
	}
	out := make([]string, len(attrKeys))
	for i, k := range attrKeys {
		if taken[short[i]] || seen[short[i]] > 1 {
			out[i] = macroFor(k)
			continue
		}
		out[i] = short[i]
	}
	return out
}

func lastSegment(k string) string {
	if i := strings.LastIndex(k, "."); i >= 0 && i+1 < len(k) {
		return k[i+1:]
	}
	return k
}

// variantRuleKey names the discovery rule of one family.
func variantRuleKey(prefix, probeType, otelName string, labels []string) string {
	return buildKey(prefix, "discovery.variants", append([]string{probeType, otelName}, labels...))
}
