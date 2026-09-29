package zabbix

import (
	"sort"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/types/datapoint"
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

// relayIdentity is the tag that tells two emitters apart on a probe no
// definition describes.
//
// The OTLP receiver relays what applications send it, under the names
// they chose. Nothing in the agent describes those names, so the key
// built for them carries the receiving probe and nothing else: two
// services reporting http.server.request.duration through one receiver
// build the same key, and the second value overwrites the first on an
// item that goes on looking healthy. That is the failure the pull
// outputs had before their discriminants were registered, on the rail
// where an item is created once and read for years.
//
// service.name is what OpenTelemetry requires an emitter to declare,
// and it is preferred over host.name on purpose: a replaced container
// keeps its service name and changes its host name, and a key built on
// the latter would mint a new item on every deployment.
const relayIdentity = "service.name"

// relayDimensions are a metric's dimensions, plus the emitter's
// identity when the metric comes from a probe no definition describes
// and the emitter named itself. A probe that has a definition is
// untouched, including prometheus_scrape, whose target already stands
// in the key for the same reason.
func relayDimensions(def *transformers.ProbeDefinition, m *transformers.MetricDefinition, tags map[string]string) []string {
	if def == nil && m == nil && tags[relayIdentity] != "" {
		return []string{relayIdentity}
	}
	return dimensions(def, m)
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
	// A metric that declares no list at all inherits the probe's. One
	// that declares an empty list says it has no dimensions, which is
	// how a machine-wide value lives on a probe whose other metrics are
	// per instance: the kernel's limits belong to the machine, not to
	// each process the probe watches.
	var source []string
	if m != nil && m.MultiInstanceLabels != nil {
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
// itemsFor renders one series as the item or items it is sent under: a
// scalar gives one, a histogram gives the two facts a scalar sink can
// hold.
func itemsFor(prefix string, def *transformers.ProbeDefinition, cm otelmapper.CacheMetric) []item {
	if cm.Histogram == nil {
		return []item{itemFor(prefix, def, cm)}
	}
	name, unit, _, params := itemParts(prefix, def, cm)
	return histogramItems(prefix, name, unit, params, cm.Histogram)
}

func itemFor(prefix string, def *transformers.ProbeDefinition, cm otelmapper.CacheMetric) item {
	name, unit, value, params := itemParts(prefix, def, cm)
	return item{
		Key:   buildKey(prefix, name, params),
		Value: strconv.FormatFloat(value, 'f', -1, 64),
		Unit:  unit,
	}
}

// itemParts resolves what a series is called, what it is worth and what
// stands between the brackets of its key.
func itemParts(prefix string, def *transformers.ProbeDefinition, cm otelmapper.CacheMetric) (name, unit string, value float64, params []string) {
	m := findMetric(def, cm.MetricName)
	name, value, unit = cm.MetricName, cm.Value, cm.Unit

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

	params = []string{cm.ProbeName}
	for _, dim := range relayDimensions(def, m, cm.Tags) {
		params = append(params, cm.Tags[dim])
	}
	params = append(params, staticAttributeValues(m)...)
	return name, unit, value, params
}

// HistogramParts are the suffixes a histogram is sent under, in the
// order the template declares them. The generator mirrors this list.
//
// A histogram has no single current value. What arrives is a count, a
// sum and a bucket ladder, and the scalar carried beside them is the
// count — so sending it under the metric's own name would put a number
// of observations under a key that says duration, which an operator
// reads as a latency and alerts on as one.
var HistogramParts = []string{"count", "sum"}

func histogramItems(prefix, name, unit string, params []string, h *datapoint.HistogramValue) []item {
	// The count is a number of observations whatever the metric
	// measures; only the sum carries the metric's own unit.
	out := []item{{
		Key:   buildKey(prefix, name+"."+HistogramParts[0], params),
		Value: strconv.FormatUint(h.Count, 10),
	}}
	if h.Sum != nil {
		out = append(out, item{
			Key:   buildKey(prefix, name+"."+HistogramParts[1], params),
			Value: strconv.FormatFloat(*h.Sum, 'f', -1, 64),
			Unit:  unit,
		})
	}
	return out
}
