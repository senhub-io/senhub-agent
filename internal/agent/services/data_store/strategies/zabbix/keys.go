package zabbix

import (
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// An item key names one series on the Zabbix side:
//
//	<prefix>.<metric>[<probe name>,<dimension>,...,<static attribute>,...]
//
// The metric is the OTel name from the probe's definition, so the same
// series is called the same thing here, on Prometheus and on OTLP; the
// dimensions are the metric's multi_instance_labels, in the order the
// definition lists them; the static attributes are the values of the
// metric's `otel.attributes`, in key order, because several internal
// metrics share one OTel name and differ only by such an attribute
// (cpu_user and cpu_system are both system.cpu.utilization, told apart
// by cpu.mode). Everything comes from the definition, so a template
// generated from the definitions (lot 2) names exactly the keys this
// code sends.
//
// Zabbix allows letters, digits, '_', '-' and '.' in the key name; a
// parameter containing ',', ']', '"' or a leading space is quoted.

type item struct {
	Key   string
	Value string
	Unit  string
}

func sanitizeKeyName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func quoteParam(p string) string {
	if p == "" {
		return p
	}
	if !strings.ContainsAny(p, ",]\"") && p[0] != ' ' {
		return p
	}
	return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"`
}

func buildKey(prefix, name string, params []string) string {
	key := sanitizeKeyName(name)
	// A vendor metric already lives under the senhub namespace
	// (senhub.system.paging.limit); prefixing it again would read
	// senhub.senhub.
	if p := sanitizeKeyName(prefix); p != "" && !strings.HasPrefix(key, p+".") {
		key = p + "." + key
	}
	if len(params) == 0 {
		return key
	}
	quoted := make([]string, len(params))
	for i, p := range params {
		quoted[i] = quoteParam(p)
	}
	return key + "[" + strings.Join(quoted, ",") + "]"
}

func findMetric(def *transformers.ProbeDefinition, name string) *transformers.MetricDefinition {
	if def == nil {
		return nil
	}
	for i := range def.Metrics {
		if def.Metrics[i].Name == name {
			return &def.Metrics[i]
		}
	}
	return nil
}

// dimensions are the labels that tell one instance of a metric from
// another. A metric that names its own replaces the definition's rather
// than adding to them: its list is an override, not an extension.
//
// Merging them was wrong in a way that showed on both ends. The process
// probe declares a per-process-id file and one aggregate count whose own
// list is the process name alone; merged, the aggregate was discovered
// per process id and every restart of the watched program orphaned its
// items. On the other side, the Windows drive metrics of logicaldisk
// name the drive letter alone, and merging gave them the device and the
// mount point they do not have, which left empty parameters in the key.
func dimensions(def *transformers.ProbeDefinition, m *transformers.MetricDefinition) []string {
	var source []string
	if m != nil && len(m.MultiInstanceLabels) > 0 {
		source = m.MultiInstanceLabels
	} else if def != nil {
		source = def.MultiInstanceLabels
	}
	out := make([]string, 0, len(source))
	seen := map[string]bool{}
	for _, l := range source {
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// staticAttributeValues lists the values of the metric's `otel.attributes`
// in attribute-key order, the part of the key that tells apart the
// internal metrics collapsed onto one OTel name.
func staticAttributeValues(m *transformers.MetricDefinition) []string {
	if m == nil || m.Otel == nil || len(m.Otel.Attributes) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Otel.Attributes))
	for k := range m.Otel.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = m.Otel.Attributes[k]
	}
	return values
}

// itemFor turns one cached series into the key and value sent to Zabbix.
//
// The value follows the OTel mapping when the definition gives one
// (unit conversion included), so a percentage arrives as a ratio and a
// duration in seconds, as on every other OTel-derived output. An enum
// metric declared with `otel.expand` is sent as its raw code under a
// single key instead of one series per state: a Zabbix value map, not a
// fan-out, is how that side reads an enum. A metric without a definition
// keeps its internal name and raw value.
func itemFor(prefix string, def *transformers.ProbeDefinition, cm otelmapper.CacheMetric) item {
	m := findMetric(def, cm.MetricName)
	name := cm.MetricName
	value, unit := cm.Value, cm.Unit

	if m != nil && m.Otel != nil && !m.Otel.Skip && m.Otel.Name != "" {
		name = m.Otel.Name
		if m.Otel.Expand == nil {
			if recs, err := otelmapper.Resolve(def, cm, otelmapper.ResolveOptions{}); err == nil && len(recs) == 1 {
				value, unit = recs[0].Value, recs[0].Unit
			}
		} else {
			unit = ""
		}
	}

	params := []string{cm.ProbeName}
	for _, dim := range dimensions(def, m) {
		params = append(params, cm.Tags[dim])
	}
	params = append(params, staticAttributeValues(m)...)
	return item{
		Key:   buildKey(prefix, name, params),
		Value: strconv.FormatFloat(value, 'f', -1, 64),
		Unit:  unit,
	}
}
