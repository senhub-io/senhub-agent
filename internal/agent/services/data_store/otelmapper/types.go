// Package otelmapper resolves cache entries to OTel-shaped records.
//
// This package owns the canonical OTel data types (OtelRecord, CacheMetric)
// and the Resolve function that walks a YAML probe definition to produce
// records ready for serialization. It is consumed by both the Prometheus
// text-exposition mapper (data_store/strategies/http/prometheus) and the
// future OTLP/gRPC push exporter (data_store/strategies/otlp), so neither
// sink depends on the other — the OTel data flow is the single shared
// contract.
//
// Design rule: nothing in this package may reference Prometheus,
// VictoriaMetrics, OTLP, or any specific transport. It speaks only OTel.
package otelmapper

import (
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// OtelRecord is a resolved, OTel-shaped data point ready for transport-
// specific serialization (Prometheus text exposition, OTLP proto, etc.).
//
// Values here are AFTER unit conversions (% → ratio, MB → By, etc.) —
// downstream serializers write them as-is into their respective formats.
//
// One CacheMetric can produce multiple OtelRecords via the OtelMapping's
// `expand` directive (one per hw.state value, for instance).
type OtelRecord struct {
	// OTel name (with dots, before any transport-specific transformation).
	// Example: "system.cpu.time", "senhub.netscaler.lbvserver.status"
	Name string

	// OTel unit (UCUM). Example: "s", "By", "1", "{packet}", "bit/s"
	Unit string

	// OTel metric type: "counter", "gauge", "updowncounter", "histogram"
	Type string

	// Attributes merged from (1) static `otel.attributes` in the YAML,
	// (2) tag_to_attribute mappings from probe tags, (3) the
	// expand-produced attribute (e.g. hw.state=ok), (4) systematic labels
	// (probe_name, probe_type, custom_tags). Names retain their OTel
	// dotted form; transport-specific encoders apply their conventions
	// (Prometheus dots → underscores, OTLP keeps dots).
	Attributes map[string]string

	// Value after unit/scale conversion. Ready to write.
	Value float64

	// Description copied from the YAML (used for the Prometheus `# HELP`
	// line and the OTel metric description field).
	Description string
}

// CacheMetric is the minimal shape Resolve consumes. It mirrors the
// http strategy's CachedMetric struct without depending on it, keeping
// otelmapper free of import cycles back into specific strategies.
type CacheMetric struct {
	// ProbeName is the instance name from config (e.g. "netscaler-prod-paris").
	// Used as the probe_name label and to route to its MetricDefinition.
	ProbeName string

	// ProbeType is the technical registry key (e.g. "netscaler"). Used to
	// look up the YAML definition.
	ProbeType string

	// MetricName is the internal bus key (e.g. "netscaler.lbvserver.state").
	MetricName string

	// Value is the raw value stored in the cache (already coerced to
	// float64 by the caller; non-numeric values must be filtered before
	// reaching here).
	Value float64

	// Unit as stored in the cache (probe-side: "%", "MB", "Mbits/s", "s", etc.)
	Unit string

	// Tags attached to the data point (discriminants + contextual + systematic).
	Tags map[string]string
}

// ResolveOptions tunes the resolver's per-scrape behavior. Currently used
// to honor storage[].params.prometheus.include_probe_tags from the agent
// configuration: when false, tags that are not explicitly mapped via
// tag_to_attribute or declared as systematic (probe_name, probe_type) are
// NOT propagated as attributes — keeping the dimension set bounded for
// operators who prefer to inject custom dimensions on the consumer side.
type ResolveOptions struct {
	// IncludeProbeTags controls whether unmapped tags on the cache entry
	// are passed through as OTel attributes. Defaults to true via the
	// zero-value-ResolveOptions caller path.
	IncludeProbeTags bool
}

// DefaultResolveOptions returns the resolver's permissive default
// (propagate everything) — used in tests and as a back-compat fallback
// when the caller hasn't wired its config through.
func DefaultResolveOptions() ResolveOptions {
	return ResolveOptions{IncludeProbeTags: true}
}

// CacheReader is the minimal interface consumers expose to Resolve so it
// can iterate over the live cache without depending on a specific
// strategy implementation.
type CacheReader interface {
	GetAll() []CacheMetric
}

// DefinitionLookup resolves a probe type (registry key) to its YAML
// definition. In production this is implemented by transformers.TransformerRegistry;
// in tests a simple map-backed implementation suffices.
type DefinitionLookup interface {
	GetProbeDefinition(probeType string) *transformers.ProbeDefinition
}
