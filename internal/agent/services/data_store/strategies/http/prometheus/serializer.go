package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// SerializeOptions tweaks the output format (reserved for future flags,
// e.g. timestamps, OpenMetrics vs classic, custom prefix).
type SerializeOptions struct {
	// IncludeTimestamp, when true, appends the record timestamp (unix ms)
	// to each metric line. Most Prometheus users want this OFF (the scraper
	// records scrape time). OFF by default.
	IncludeTimestamp bool
}

// SerializeToTextExposition writes the Prometheus text exposition format v0.0.4.
// Records are grouped by their resolved Prometheus name and emitted with a
// single `# HELP` and `# TYPE` header per group, followed by all label
// variants of the metric.
//
// Reference: https://prometheus.io/docs/instrumenting/exposition_formats/
func SerializeToTextExposition(records []otelmapper.OtelRecord, w io.Writer, opts SerializeOptions) error {
	// Group records by their final Prometheus metric name. Two OTel metrics
	// collapsed into the same name (common pattern — e.g. hw.status emitted
	// from drive.health AND drive.failure_predicted) are merged here.
	type group struct {
		promName    string
		promType    string
		help        string
		unit        string
		firstOTel   string // first OTel name that produced this Prom name (for collision warnings)
		rows        []otelmapper.OtelRecord
	}
	groups := map[string]*group{}
	// Preserve first-seen order for stable, human-friendly output.
	var order []string

	for _, r := range records {
		promName := OTelNameToPromName(r.Name, r.Unit, r.Type)
		g, ok := groups[promName]
		if !ok {
			g = &group{
				promName:  promName,
				promType:  PromType(r.Type),
				help:      r.Description,
				unit:      r.Unit,
				firstOTel: r.Name,
			}
			groups[promName] = g
			order = append(order, promName)
		} else {
			// Detect conflicts between records that collapse to the same Prom
			// name. We pick a deterministic winner (first-seen) but report
			// any divergence via the dedicated helpers so operators have a
			// chance to fix the YAML.
			if got := PromType(r.Type); got != g.promType {
				warnTypeConflict(promName, g.firstOTel, g.promType, r.Name, got)
			}
			if r.Unit != g.unit {
				warnUnitConflict(promName, g.firstOTel, g.unit, r.Name, r.Unit)
			}
			if g.help == "" && r.Description != "" {
				g.help = r.Description
			}
		}
		g.rows = append(g.rows, r)
	}

	for _, promName := range order {
		g := groups[promName]
		if err := writeGroup(w, g.promName, g.promType, g.help, g.rows, opts); err != nil {
			return err
		}
	}
	return nil
}

func writeGroup(w io.Writer, name, promType, help string, rows []otelmapper.OtelRecord, opts SerializeOptions) error {
	if help != "" {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, HelpString(help)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, promType); err != nil {
		return err
	}

	// Sort rows deterministically for stable output (makes scraper diffs
	// and test assertions easier).
	sort.Slice(rows, func(i, j int) bool {
		return labelString(rows[i].Attributes) < labelString(rows[j].Attributes)
	})

	for _, r := range rows {
		labelKeys := make([]string, 0, len(r.Attributes))
		promLabels := make(map[string]string, len(r.Attributes))
		// Track which OTel attribute originally produced each Prom label so
		// we can warn (once) when two distinct attribute names collapse to
		// the same Prom label after dot→underscore translation.
		labelOrigin := make(map[string]string, len(r.Attributes))

		// Iterate in sorted key order so the "kept vs dropped" decision in
		// a collision is deterministic across builds. Without this, Go map
		// iteration randomization would produce non-reproducible warnings
		// and (more visibly) flip which value lands on the kept label.
		sortedAttrKeys := make([]string, 0, len(r.Attributes))
		for k := range r.Attributes {
			sortedAttrKeys = append(sortedAttrKeys, k)
		}
		sort.Strings(sortedAttrKeys)

		for _, k := range sortedAttrKeys {
			v := r.Attributes[k]
			pk := OTelAttributeToPromLabel(k)
			if existing, clashes := labelOrigin[pk]; clashes && existing != k {
				warnLabelCollision(name, pk, existing, k)
				continue
			}
			labelKeys = append(labelKeys, pk)
			promLabels[pk] = v
			labelOrigin[pk] = k
		}
		sort.Strings(labelKeys)
		labels := FormatLabels(labelKeys, promLabels)
		valueStr := formatValue(r.Value)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", name, labels, valueStr); err != nil {
			return err
		}
	}
	// Blank line between groups for readability (not required by the spec).
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return nil
}

// labelString produces a stable string representation of a label set
// used for sorting rows within a group.
func labelString(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf []byte
	for _, k := range keys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, attrs[k]...)
		buf = append(buf, ',')
	}
	return string(buf)
}

// formatValue formats a float64 per the Prometheus exposition format.
// Special values: +Inf → "+Inf", -Inf → "-Inf", NaN → "NaN".
func formatValue(v float64) string {
	// Integer-valued floats render without decimal for readability.
	if v == float64(int64(v)) && v > -1e15 && v < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// serializerWarnDedup memoizes (kind, key) tuples we've already warned
// about so a single misconfiguration doesn't flood the log on every scrape.
// Cleared only by process restart — the situations being detected are YAML
// drift and don't change at runtime.
var serializerWarnDedup sync.Map

// SerializerWarnFunc is invoked when the serializer detects a YAML drift
// problem. Defaults to a no-op so the serializer stays self-contained
// (the bridge wires it to the agent logger via SetSerializerWarnFunc).
type SerializerWarnFunc func(format string, args ...interface{})

// noopSerializerWarn is the default value installed in serializerWarnFn.
// It is a real value (not nil) so we can read through atomic.Pointer
// without a nil check on the hot path.
var noopSerializerWarn SerializerWarnFunc = func(string, ...interface{}) {}

// serializerWarnFn holds the current warn function as an atomic pointer.
// Read on every scrape (potentially many concurrent goroutines on a
// busy agent), written rarely (once at startup, plus tests). Atomic.Pointer
// gives us the lock-free read path with safe replacement.
var serializerWarnFn atomic.Pointer[SerializerWarnFunc]

func init() {
	serializerWarnFn.Store(&noopSerializerWarn)
}

// SetSerializerWarnFunc lets the host application install a logger for
// the serializer drift warnings. Safe to call concurrently with
// serialization (atomic pointer swap). Passing nil restores the
// default no-op.
func SetSerializerWarnFunc(fn SerializerWarnFunc) {
	if fn == nil {
		serializerWarnFn.Store(&noopSerializerWarn)
		return
	}
	// Wrap in a fresh allocation so the pointer we expose is stable for the
	// lifetime of this Set call — required by atomic.Pointer semantics.
	stored := fn
	serializerWarnFn.Store(&stored)
}

// callSerializerWarn invokes the currently-installed warn function.
// Pulled out as a helper so call sites stay readable.
func callSerializerWarn(format string, args ...interface{}) {
	(*serializerWarnFn.Load())(format, args...)
}

func warnTypeConflict(promName, firstOTel, firstType, otherOTel, otherType string) {
	key := "type:" + promName
	if _, seen := serializerWarnDedup.LoadOrStore(key, struct{}{}); seen {
		return
	}
	callSerializerWarn(
		"prometheus serializer: OTel name conflict — %q (Prom name %q) declared as %q earlier (from %q) but later seen as %q. Picking the first; fix the YAML to make types consistent.",
		otherOTel, promName, firstType, firstOTel, otherType,
	)
}

func warnUnitConflict(promName, firstOTel, firstUnit, otherOTel, otherUnit string) {
	key := "unit:" + promName
	if _, seen := serializerWarnDedup.LoadOrStore(key, struct{}{}); seen {
		return
	}
	callSerializerWarn(
		"prometheus serializer: OTel unit conflict — %q (Prom name %q) declared with unit %q earlier (from %q) but later seen as %q. Help/TYPE preserved from first; fix the YAML.",
		otherOTel, promName, firstUnit, firstOTel, otherUnit,
	)
}

// warnLabelCollision is invoked from writeGroup when two distinct OTel
// attribute names collapse to the same Prometheus label after dot→underscore
// translation (e.g. "cpu.mode" and "cpu_mode").
func warnLabelCollision(promName, promLabel, kept, dropped string) {
	key := "label:" + promName + ":" + promLabel
	if _, seen := serializerWarnDedup.LoadOrStore(key, struct{}{}); seen {
		return
	}
	callSerializerWarn(
		"prometheus serializer: attribute collision on metric %q — both %q and %q map to label %q. Keeping %q; fix the YAML so attributes do not alias.",
		promName, kept, dropped, promLabel, kept,
	)
}

// resetSerializerWarnDedupForTest clears the per-(kind,key) dedup map.
// For test isolation only.
func resetSerializerWarnDedupForTest() {
	serializerWarnDedup.Range(func(k, _ interface{}) bool {
		serializerWarnDedup.Delete(k)
		return true
	})
}

