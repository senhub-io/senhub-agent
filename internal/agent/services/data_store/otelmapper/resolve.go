package otelmapper

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// Resolve converts a single CacheMetric into zero, one or more OtelRecord(s)
// ready for transport-specific serialization.
//
// It looks up the MetricDefinition in the provided ProbeDefinition (by
// matching the internal name) and applies:
//   - Skip: returns nil if otel.skip is true (explicit opt-out)
//   - Static attributes (otel.attributes)
//   - Tag → attribute mapping (tag_to_attribute)
//   - Unit conversions (% → ratio, MB → By, etc.) and explicit ValueScale
//   - Expand: produces N records when otel.expand is declared, plus a
//     synthetic "unknown" record when the raw value matches no declared
//     state (only if the YAML didn't enumerate "unknown" itself)
//   - Systematic labels probe_name, probe_type, and best-effort
//     passthrough of remaining tags when opts.IncludeProbeTags is true
//
// Returns (nil, nil) when the metric is explicitly skipped. Returns an
// error if no matching MetricDefinition or no otel section is found —
// callers may choose to skip-with-warn rather than fail (the Prometheus
// path does this; OTLP does the same).
func Resolve(def *transformers.ProbeDefinition, m CacheMetric, opts ResolveOptions) ([]OtelRecord, error) {
	// OTLP-ingested metrics arrive already OTel-shaped: the otlp_receiver
	// probe decodes an inbound OTLP stream straight into datapoints whose
	// name is a canonical OTel name. There is no per-probe transformer
	// definition to look up (the names are arbitrary external identifiers),
	// so pass them through as-is. Keyed on the neutral metric_type marker —
	// not a probe package — so the mapper stays probe-agnostic.
	if m.Tags[metricTypeTag] == MetricTypeOTLPIngest {
		return resolveOTLPIngested(m, opts), nil
	}

	// Typed pass-through: a datapoint that already carries a canonical OTel
	// name and declares its OTel type in the otel_type tag has no
	// transformer row to look up. snmp_poll uses this for its dynamic
	// per-OID long-tail metrics (issue #207); the mechanism is probe-neutral
	// — any probe that pre-shapes a name + type can opt in.
	if otelType := m.Tags[otelTypeTag]; otelType != "" {
		return resolveTypedPassthrough(m, otelType, opts), nil
	}

	if def == nil {
		return nil, fmt.Errorf("no probe definition for probe_type=%q", m.ProbeType)
	}

	mdef := findMetricDefinition(def, m.MetricName)
	if mdef == nil {
		return nil, fmt.Errorf("no metric definition for %q in probe_type=%q", m.MetricName, m.ProbeType)
	}
	if mdef.Otel == nil {
		return nil, fmt.Errorf("metric %q in probe_type=%q has no otel mapping", m.MetricName, m.ProbeType)
	}
	if mdef.Otel.Skip {
		return nil, nil // explicit skip, not an error
	}

	// Build base attributes: static (from YAML) + tag_to_attribute-mapped tags.
	baseAttrs := map[string]string{}
	for k, v := range mdef.Otel.Attributes {
		baseAttrs[k] = v
	}
	for tagName, attrName := range mdef.TagToAttribute {
		if val, ok := m.Tags[tagName]; ok && val != "" {
			baseAttrs[attrName] = val
		}
	}

	// Systematic labels: probe_name and probe_type are ALWAYS emitted on
	// probe metrics. They surface as labels on Prometheus and as
	// attributes on OTLP — same names, same semantics.
	baseAttrs["probe_name"] = m.ProbeName
	if m.ProbeType != "" {
		baseAttrs["probe_type"] = m.ProbeType
	}

	// Passthrough: propagate remaining tags that aren't already mapped, when
	// the resolver was configured with IncludeProbeTags. Excludes systematic
	// bookkeeping tags that shouldn't leak as labels.
	if opts.IncludeProbeTags {
		for tagName, tagVal := range m.Tags {
			if tagVal == "" {
				continue
			}
			if isSystemTag(tagName) {
				continue
			}
			if _, alreadyMapped := mdef.TagToAttribute[tagName]; alreadyMapped {
				continue
			}
			// Don't overwrite static attributes declared in the YAML.
			if _, isStatic := baseAttrs[tagName]; isStatic {
				continue
			}
			baseAttrs[tagName] = tagVal
		}
	}

	// Apply value conversion based on source unit → target OTel unit.
	// Primary source: m.Unit (set by data_store.applyUnitCorrections from
	// the YAML transformer before fan-out to strategies — see
	// data_store.go). YAML fallback kept here as defense in depth: it
	// keeps Resolve self-sufficient for direct callers (tests, future
	// non-data_store entry points) without changing the production path.
	sourceUnit := m.Unit
	if sourceUnit == "" {
		sourceUnit = mdef.Unit
	}
	value := convertValue(m.Value, sourceUnit, mdef.Otel.Unit, mdef.Otel.ValueScale)

	// Simple case: no expand — emit a single record.
	if mdef.Otel.Expand == nil {
		return []OtelRecord{{
			Name:        mdef.Otel.Name,
			Unit:        mdef.Otel.Unit,
			Type:        mdef.Otel.Type,
			Attributes:  baseAttrs,
			Value:       value,
			Description: mdef.Description,
		}}, nil
	}

	// Expand: emit one record per mapping entry with value=1 if the raw
	// (pre-conversion) cache value matches, else 0. If the raw value
	// matches NONE of the declared codes, emit an additional synthetic
	// record with attribute=unknown so dashboards can distinguish
	// "probe broken / lookup out of date" from "all states are 0".
	rawCode := int(m.Value)
	records := make([]OtelRecord, 0, len(mdef.Otel.Expand.Mapping)+1)
	matched := false
	for stateName, stateCode := range mdef.Otel.Expand.Mapping {
		attrs := make(map[string]string, len(baseAttrs)+1)
		for k, v := range baseAttrs {
			attrs[k] = v
		}
		attrs[mdef.Otel.Expand.Attribute] = stateName
		var matchVal float64
		if rawCode == stateCode {
			matchVal = 1
			matched = true
		}
		records = append(records, OtelRecord{
			Name:        mdef.Otel.Name,
			Unit:        mdef.Otel.Unit,
			Type:        mdef.Otel.Type,
			Attributes:  attrs,
			Value:       matchVal,
			Description: mdef.Description,
		})
	}
	// Synthetic "unknown" record fires only if the raw value didn't match
	// any declared mapping AND the YAML didn't already enumerate "unknown".
	// Note: the expand attribute is set unconditionally to "unknown",
	// overwriting any value that may have been in baseAttrs under the same
	// key — same convention as the matched-state branch above.
	if !matched {
		if _, alreadyDeclared := mdef.Otel.Expand.Mapping["unknown"]; !alreadyDeclared {
			attrs := make(map[string]string, len(baseAttrs)+1)
			for k, v := range baseAttrs {
				attrs[k] = v
			}
			attrs[mdef.Otel.Expand.Attribute] = "unknown"
			records = append(records, OtelRecord{
				Name:        mdef.Otel.Name,
				Unit:        mdef.Otel.Unit,
				Type:        mdef.Otel.Type,
				Attributes:  attrs,
				Value:       1,
				Description: mdef.Description,
			})
		}
	}
	return records, nil
}

const (
	// metricTypeTag is the internal tag key carrying a datapoint's family
	// / origin marker.
	metricTypeTag = "metric_type"

	// MetricTypeOTLPIngest marks datapoints decoded from an inbound OTLP
	// stream by the otlp_receiver probe. Resolve passes these through
	// without a transformer definition lookup. The otlp_receiver probe
	// sets this value on every datapoint it emits.
	MetricTypeOTLPIngest = "otlp_ingest"

	// otelTypeTag, when present on a datapoint, opts it into a typed
	// pass-through: the name is taken as a canonical OTel name and this tag
	// carries the OTel instrument type (counter / gauge / updowncounter).
	// Used for metrics that cannot be pre-enumerated in a transformer YAML
	// (snmp_poll dynamic per-OID metrics, #207).
	otelTypeTag = "otel_type"
)

// resolveTypedPassthrough emits an OtelRecord for a datapoint that already
// carries a canonical OTel name and declares its instrument type in the
// otel_type tag, with no transformer definition. Value and unit are taken
// as received; an unrecognised type falls back to gauge (the safe default).
func resolveTypedPassthrough(m CacheMetric, otelType string, opts ResolveOptions) []OtelRecord {
	switch otelType {
	case "counter", "gauge", "updowncounter":
		// declared type accepted as-is
	default:
		otelType = "gauge"
	}

	attrs := map[string]string{"probe_name": m.ProbeName}
	if m.ProbeType != "" {
		attrs["probe_type"] = m.ProbeType
	}
	if opts.IncludeProbeTags {
		for tagName, tagVal := range m.Tags {
			if tagVal == "" || isSystemTag(tagName) || tagName == metricTypeTag || tagName == otelTypeTag {
				continue
			}
			attrs[tagName] = tagVal
		}
	}
	return []OtelRecord{{
		Name:        m.MetricName,
		Unit:        m.Unit,
		Type:        otelType,
		Attributes:  attrs,
		Value:       m.Value,
		Description: "pass-through metric (no transformer definition)",
	}}
}

// resolveOTLPIngested passes an already-OTel-shaped, externally-ingested
// metric straight through to an OtelRecord: its name is a canonical OTel
// name and it has no transformer definition. Value and unit are taken as
// received. Type is reported as gauge — the inbound OTLP gauge/sum
// distinction is not preserved across the flat datapoint bus, and gauge
// is the safe re-export default.
func resolveOTLPIngested(m CacheMetric, opts ResolveOptions) []OtelRecord {
	attrs := map[string]string{"probe_name": m.ProbeName}
	if m.ProbeType != "" {
		attrs["probe_type"] = m.ProbeType
	}
	if opts.IncludeProbeTags {
		for tagName, tagVal := range m.Tags {
			if tagVal == "" || isSystemTag(tagName) || tagName == metricTypeTag {
				continue
			}
			attrs[tagName] = tagVal
		}
	}
	return []OtelRecord{{
		Name:        m.MetricName,
		Unit:        m.Unit,
		Type:        "gauge",
		Attributes:  attrs,
		Value:       m.Value,
		Description: "OTLP-ingested metric",
	}}
}

// findMetricDefinition locates the MetricDefinition matching an internal
// metric name. Simple linear scan — probe definitions are small (< 100
// metrics), so no need for indexing.
func findMetricDefinition(def *transformers.ProbeDefinition, metricName string) *transformers.MetricDefinition {
	for i := range def.Metrics {
		if def.Metrics[i].Name == metricName {
			return &def.Metrics[i]
		}
	}
	return nil
}

// isSystemTag returns true when a tag should not be emitted as an OTel
// attribute. These are internal bookkeeping tags injected by the cache or
// the agent framework — Resolve handles them explicitly via baseAttrs.
func isSystemTag(tag string) bool {
	switch tag {
	case "probe_name", "probe_type":
		return true
	}
	return false
}
