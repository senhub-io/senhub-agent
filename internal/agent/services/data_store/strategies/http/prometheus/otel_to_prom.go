// Package prometheus implements the OTel → Prometheus text exposition mapper.
//
// The agent emits data points through probes into a shared cache
// (MetricCache). This package reads those cache entries, resolves them to
// OTel-shaped records via the YAML transformer definitions (section `otel:`
// in each probe YAML), and serializes them in Prometheus text exposition
// format (v0.0.4) for the /metrics endpoint.
//
// Conversion rules implemented here follow the official OTel → Prometheus
// compatibility specification:
// https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/
package prometheus

import (
	"fmt"
	"regexp"
	"strings"
)

// Prefix applied to every metric name in our exposition, independent of the
// OTel namespace (`system.*`, `senhub.*`, etc.). Frozen by design decision —
// see docs/developer-guide/prometheus/IMPLEMENTATION-PLAN.md §12.
const MetricPrefix = "senhub"

// promNameInvalidChars matches characters not allowed in Prometheus metric
// or label names (regex `[a-zA-Z_:][a-zA-Z0-9_:]*` for metrics, same without
// colon for labels). We normalize to a conservative set that works for both.
var promNameInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// collapseUnderscores reduces consecutive underscores to a single one.
var collapseUnderscores = regexp.MustCompile(`_+`)

// OTelNameToPromName converts an OTel metric name + unit + type to a
// Prometheus metric name following the compatibility spec.
//
// Rules applied (in order):
//  1. Prepend `senhub_` prefix.
//  2. Dots and other non-[a-zA-Z0-9_] → `_`.
//  3. Collapse consecutive `_` to a single `_`.
//  4. Append unit suffix (see unitSuffix) unless already present.
//  5. For counters, append `_total` unless already present.
//
// Examples:
//
//	system.cpu.time, s, counter    → senhub_system_cpu_time_seconds_total
//	system.memory.usage, By, ugce  → senhub_system_memory_usage_bytes
//	system.cpu.utilization, 1, g   → senhub_system_cpu_utilization_ratio
//	senhub.veeam.job.objects, {object}, g → senhub_veeam_job_objects
func OTelNameToPromName(otelName, unit, metricType string) string {
	// OTel names under `senhub.*` already carry the namespace — don't double-prefix.
	// Other namespaces (system.*, hw.*, etc.) get the senhub_ prefix prepended
	// so every series exposed by this agent is namespaced to senhub.
	var name string
	if strings.HasPrefix(otelName, MetricPrefix+".") {
		name = otelName
	} else {
		name = MetricPrefix + "." + otelName
	}

	// Sanitize: any non-identifier char → underscore
	name = promNameInvalidChars.ReplaceAllString(name, "_")
	name = collapseUnderscores.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")

	// Append unit suffix if it adds information and isn't already there.
	// Special case: the "_ratio" suffix (OTel unit "1") is reserved for
	// gauge-type metrics whose value is in [0,1]. Applying it to status
	// metrics (e.g. hw.status, which is updowncounter with value 0/1 per
	// state and emitted as a Prometheus gauge) would mislead users into
	// expecting fractional readings — they're enumerated booleans. Skip
	// the suffix when the OTel type is updowncounter or counter.
	if suffix := unitSuffix(unit); suffix != "" {
		skipSuffix := suffix == "ratio" && (strings.EqualFold(metricType, "updowncounter") || strings.EqualFold(metricType, "counter"))
		if !skipSuffix && !strings.HasSuffix(name, "_"+suffix) {
			name = name + "_" + suffix
		}
	}

	// Counters get `_total` suffix
	if isCounterType(metricType) && !strings.HasSuffix(name, "_total") {
		name = name + "_total"
	}

	return name
}

// OTelAttributeToPromLabel converts an OTel attribute name (possibly dotted,
// e.g. "cpu.mode", "system.memory.state") to a Prometheus label name.
//
// Rules: dots / any non-identifier char → `_`, consecutive `_` collapsed.
func OTelAttributeToPromLabel(attrName string) string {
	label := promNameInvalidChars.ReplaceAllString(attrName, "_")
	label = collapseUnderscores.ReplaceAllString(label, "_")
	return strings.Trim(label, "_")
}

// unitSuffix returns the Prometheus name suffix to append for a given OTel
// UCUM unit, or "" if no suffix should be added.
//
// Reference: OTel → Prometheus unit conversion table
// (https://opentelemetry.io/docs/specs/otel/compatibility/prometheus_and_openmetrics/)
func unitSuffix(unit string) string {
	// Annotations in braces are dropped (e.g. `{packet}`, `{error}`, `{day}`).
	// These units describe semantic meaning but have no Prometheus suffix.
	if strings.HasPrefix(unit, "{") && strings.HasSuffix(unit, "}") {
		return ""
	}

	// Well-known UCUM units with direct Prometheus equivalents.
	switch unit {
	case "":
		return ""
	case "s":
		return "seconds"
	case "ms":
		return "milliseconds"
	case "us", "μs":
		return "microseconds"
	case "ns":
		return "nanoseconds"
	case "By":
		return "bytes"
	case "KiBy":
		return "kibibytes"
	case "MiBy":
		return "mebibytes"
	case "GiBy":
		return "gibibytes"
	case "Hz":
		return "hertz"
	case "W":
		return "watts"
	case "J":
		return "joules"
	case "V":
		return "volts"
	case "A":
		return "amperes"
	case "Cel":
		return "celsius"
	case "dBm":
		return "dbm"
	case "1":
		return "ratio"
	}

	// Rate units of form "unit/s" or "unit/unit":
	//   `bit/s` → `bits_per_second`
	//   `By/s`  → `bytes_per_second`
	//   `1/s`   → `per_second`
	//   `{packet}/s` → `per_second` (annotation dropped)
	if idx := strings.Index(unit, "/"); idx >= 0 {
		num := unit[:idx]
		den := unit[idx+1:]
		numSuffix := unitSuffixPart(num)
		denSuffix := unitSuffixPart(den)
		if numSuffix == "" && denSuffix == "" {
			return ""
		}
		if numSuffix == "" {
			return "per_" + denSuffix
		}
		if denSuffix == "" {
			return numSuffix
		}
		return numSuffix + "_per_" + denSuffix
	}

	// Fallback: sanitize raw unit as best-effort suffix
	cleaned := promNameInvalidChars.ReplaceAllString(unit, "_")
	cleaned = collapseUnderscores.ReplaceAllString(cleaned, "_")
	cleaned = strings.Trim(cleaned, "_")
	return strings.ToLower(cleaned)
}

// unitSuffixPart returns the suffix for a single unit component (numerator
// or denominator of a compound unit like "By/s").
func unitSuffixPart(part string) string {
	// Annotations dropped
	if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
		return ""
	}
	switch part {
	case "":
		return ""
	case "1":
		return "" // dimensionless numerator/denominator contributes no word
	case "s":
		return "second"
	case "ms":
		return "millisecond"
	case "bit":
		return "bits"
	case "By":
		return "bytes"
	case "Hz":
		return "hertz"
	}
	// Fallback
	return strings.ToLower(promNameInvalidChars.ReplaceAllString(part, "_"))
}

// isCounterType returns true when the OTel type string represents a monotonic
// counter requiring the `_total` Prometheus suffix.
//
// Note: `updowncounter` is NOT a Prometheus counter — it's a gauge that can
// increase or decrease, emitted as a Prometheus gauge (no `_total` suffix).
func isCounterType(t string) bool {
	return strings.EqualFold(t, "counter")
}

// PromType returns the Prometheus TYPE line value for a given OTel type.
// Used in the text exposition `# TYPE <name> <type>` header.
func PromType(otelType string) string {
	switch strings.ToLower(otelType) {
	case "counter":
		return "counter"
	case "updowncounter", "gauge":
		return "gauge"
	case "histogram":
		return "histogram"
	default:
		return "gauge" // safe default
	}
}

// ConvertValue applies unit-based scaling to the raw cache value so that it
// matches the OTel unit declared in the YAML.
//
// Conversions applied:
//   - Percent → ratio (sourceUnit ∈ {%, percent}, otelUnit == "1"): ÷100
//   - Kilobytes → bytes (sourceUnit ∈ {KB, kb, kilobyte}, otelUnit == "By"): ×1024
//   - Megabytes → bytes (sourceUnit ∈ {MB, mb, megabyte}, otelUnit == "By"): ×1048576
//   - Gigabytes → bytes: ×1073741824
//   - Milliseconds → seconds (ms → s): ÷1000
//   - Microseconds → seconds (us/μs → s): ÷1e6
//   - Megabits per second → bits per second (Mbps/Mbits/s → bit/s): ×1e6
//   - Gigabits per second → bits per second: ×1e9
//   - Hours → seconds (h → s): ×3600
//   - Days → seconds (d → s): ×86400
//
// Also applies the explicit ValueScale from the YAML if present (takes
// precedence over unit-based conversions).
func ConvertValue(raw float64, sourceUnit, otelUnit string, valueScale float64) float64 {
	// Explicit scale wins (probe-specific conversions)
	if valueScale != 0 {
		return raw * valueScale
	}

	src := strings.ToLower(strings.TrimSpace(sourceUnit))
	dst := strings.TrimSpace(otelUnit)

	// % → ratio
	if dst == "1" && (src == "%" || src == "percent") {
		return raw / 100.0
	}

	// *B → By (bytes)
	if dst == "By" {
		switch src {
		case "kb", "kib", "kibibyte", "kilobyte":
			return raw * 1024.0
		case "mb", "mib", "mebibyte", "megabyte":
			return raw * 1048576.0
		case "gb", "gib", "gibibyte", "gigabyte":
			return raw * 1073741824.0
		}
	}

	// ms/us → s
	if dst == "s" {
		switch src {
		case "ms", "millisecond", "milliseconds":
			return raw / 1000.0
		case "us", "μs", "microsecond", "microseconds":
			return raw / 1.0e6
		case "ns", "nanosecond", "nanoseconds":
			return raw / 1.0e9
		case "h", "hour", "hours":
			return raw * 3600.0
		case "d", "day", "days":
			return raw * 86400.0
		}
	}

	// Mbits/s → bit/s
	if dst == "bit/s" {
		switch src {
		case "mbits/s", "mbps", "megabits/s":
			return raw * 1.0e6
		case "gbits/s", "gbps", "gigabits/s", "gbit/s":
			return raw * 1.0e9
		}
	}

	// No conversion needed
	return raw
}

// HelpString returns a sanitized single-line help string safe for the
// Prometheus `# HELP` header. Newlines and backslashes are escaped per
// the text exposition spec.
//
// Note: unlike LabelValueString below, double quotes are NOT escaped here.
// Per the spec, the HELP value runs from the first non-space character
// after the metric name until end-of-line, with no quoting — so quotes
// inside the text are literal characters. Only backslash and newline
// need escaping.
func HelpString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// LabelValueString returns a label value safely escaped for the Prometheus
// text exposition format. Double quotes, backslashes and newlines escaped.
func LabelValueString(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// FormatLabels formats a label set into the Prometheus `{k="v",k2="v2"}`
// representation. The order of emitted labels matches the order of `keys`;
// callers in this package always pre-sort lexicographically for stable
// output. Empty values and missing keys are skipped (see body comment).
func FormatLabels(keys []string, values map[string]string) string {
	if len(keys) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, k := range keys {
		v, ok := values[k]
		if !ok || v == "" {
			// Per Prometheus convention, an empty label is equivalent to
			// the label being absent — but it can surprise PromQL writers
			// (`{foo=""}` matches differently from no `foo`). Skip empties
			// so behavior matches user intuition.
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteByte('"')
		b.WriteString(LabelValueString(v))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	if b.Len() == 2 { // only "{}"
		return ""
	}
	return b.String()
}

// Ensure the package compiles even when fmt is not used locally.
var _ = fmt.Sprint
