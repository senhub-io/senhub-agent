package prometheus

import (
	"fmt"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// CacheMetric is the minimal shape the resolver consumes. It mirrors the
// existing http.CachedMetric struct but is defined locally to avoid an
// import cycle with the parent `http` package.
type CacheMetric struct {
	// ProbeName is the instance name from config (e.g. "netscaler-prod-paris").
	// Used as the probe_name label and to route to its MetricDefinition.
	ProbeName string

	// ProbeType is the technical registry key (e.g. "netscaler"). Used to
	// look up the YAML definition. Extracted from CachedMetric.Tags["probe_type"].
	ProbeType string

	// MetricName is the internal bus key (e.g. "netscaler.lbvserver.state").
	MetricName string

	// Value is the raw value stored in the cache (may be int, float or bool-as-int).
	Value float64

	// Unit as stored in the cache (probe-side: "%", "MB", "Mbits/s", "s", etc.)
	Unit string

	// Tags attached to the data point (discriminants + contextual + systematic).
	Tags map[string]string
}

// Resolve converts a single CacheMetric into zero, one or more OtelRecord(s)
// ready for Prometheus serialization.
//
// It looks up the MetricDefinition in the provided ProbeDefinition (by
// matching the internal name) and applies:
//   - Skip: returns nil if otel.skip is true
//   - Static attributes (otel.attributes)
//   - Tag → attribute mapping (tag_to_attribute)
//   - Unit conversions (% → ratio, MB → By, etc.) and explicit ValueScale
//   - Expand: produces N records when otel.expand is declared
//   - Injects systematic labels probe_name, probe_type, and passthrough
//     of any other cache tags that are not already mapped (best-effort,
//     keeps custom_tags propagation).
//
// Returns (nil, nil) when the metric is explicitly skipped. Returns an error
// if no matching MetricDefinition or no otel section is found — per the
// design decision, every metric must have an explicit OTel mapping or skip.
func Resolve(def *transformers.ProbeDefinition, m CacheMetric) ([]OtelRecord, error) {
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

	// Systematic labels: probe_name and probe_type are ALWAYS emitted on probe
	// metrics. They're namespaced under `senhub.probe.*` for OTel clarity but
	// serialize to `probe_name` / `probe_type` in Prometheus (OTelAttributeToPromLabel
	// strips the `senhub_probe_` prefix when it's a systematic label — see the
	// serializer).
	baseAttrs["probe_name"] = m.ProbeName
	if m.ProbeType != "" {
		baseAttrs["probe_type"] = m.ProbeType
	}

	// Passthrough: propagate any remaining tag that isn't already mapped.
	// Excludes systematic bookkeeping tags that shouldn't leak as labels.
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

	// Apply value conversion.
	value := ConvertValue(m.Value, m.Unit, mdef.Otel.Unit, mdef.Otel.ValueScale)

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
	// (pre-conversion) cache value matches, else 0.
	rawCode := int(m.Value)
	records := make([]OtelRecord, 0, len(mdef.Otel.Expand.Mapping))
	for stateName, stateCode := range mdef.Otel.Expand.Mapping {
		attrs := make(map[string]string, len(baseAttrs)+1)
		for k, v := range baseAttrs {
			attrs[k] = v
		}
		attrs[mdef.Otel.Expand.Attribute] = stateName
		var matchVal float64
		if rawCode == stateCode {
			matchVal = 1
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
	return records, nil
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
// the agent framework.
func isSystemTag(tag string) bool {
	switch tag {
	case "probe_name", "probe_type":
		// Already explicitly handled by Resolve (added via baseAttrs).
		return true
	}
	return false
}
