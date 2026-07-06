package otlpreceiver

import (
	"fmt"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
)

// flattenResourceMetrics converts the OTLP wire form of a metrics
// payload into the agent's flat DataPoint slice. Only the number
// data point families (Gauge, Sum) are flattened — histograms,
// exponential histograms and summaries carry no single scalar Value
// that maps to a DataPoint, so they are reported via the returned
// dropped count rather than silently lost.
//
// Resource attributes are folded onto every emitted datapoint as
// tags so a downstream sink can still group by host.name / service.*
// even though the agent has no per-process Resource concept for an
// ingested external stream. Per-datapoint attributes win over
// resource attributes on a key collision (the more specific scope).
func flattenResourceMetrics(resourceMetrics []*metricpb.ResourceMetrics) (points []data_store.DataPoint, dropped int) {
	for _, rm := range resourceMetrics {
		resourceTags := attributesToTags(resourceAttributes(rm.GetResource()))
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				pts, drop := flattenMetric(m, resourceTags)
				points = append(points, pts...)
				dropped += drop
			}
		}
	}
	return points, dropped
}

func resourceAttributes(r *resourcepb.Resource) []*commonpb.KeyValue {
	if r == nil {
		return nil
	}
	return r.GetAttributes()
}

func flattenMetric(m *metricpb.Metric, resourceTags []tags.Tag) (points []data_store.DataPoint, dropped int) {
	name := m.GetName()
	if name == "" {
		return nil, 0
	}

	unit := m.GetUnit()

	switch data := m.GetData().(type) {
	case *metricpb.Metric_Gauge:
		for _, dp := range data.Gauge.GetDataPoints() {
			points = append(points, numberPointToDataPoint(name, unit, "gauge", dp, resourceTags))
		}
	case *metricpb.Metric_Sum:
		otelType := sumInstrumentType(data.Sum)
		for _, dp := range data.Sum.GetDataPoints() {
			points = append(points, numberPointToDataPoint(name, unit, otelType, dp, resourceTags))
		}
	default:
		// Histogram / ExponentialHistogram / Summary / unset: no scalar
		// value to map onto a DataPoint. Count them so the caller can
		// surface the drop instead of pretending the export was lossless.
		dropped += countNonNumberPoints(m)
	}
	return points, dropped
}

// sumInstrumentType maps an inbound OTLP Sum onto the agent's internal
// OTel instrument type. A cumulative monotonic sum is a counter; a
// cumulative non-monotonic sum is an updowncounter. Delta-temporality
// sums cannot be re-exported faithfully — the agent's exporters emit
// only cumulative temporality — so they degrade to gauge, which
// preserves the raw value without asserting false cumulative-counter
// semantics. Full delta pass-through would need a temporality field on
// OtelRecord threaded through both serializers (deferred follow-up).
func sumInstrumentType(sum *metricpb.Sum) string {
	if sum.GetAggregationTemporality() == metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		return "gauge"
	}
	if sum.GetIsMonotonic() {
		return "counter"
	}
	return "updowncounter"
}

func countNonNumberPoints(m *metricpb.Metric) int {
	switch data := m.GetData().(type) {
	case *metricpb.Metric_Histogram:
		return len(data.Histogram.GetDataPoints())
	case *metricpb.Metric_ExponentialHistogram:
		return len(data.ExponentialHistogram.GetDataPoints())
	case *metricpb.Metric_Summary:
		return len(data.Summary.GetDataPoints())
	default:
		return 1
	}
}

func numberPointToDataPoint(name, unit, otelType string, dp *metricpb.NumberDataPoint, resourceTags []tags.Tag) data_store.DataPoint {
	var value float64
	switch v := dp.GetValue().(type) {
	case *metricpb.NumberDataPoint_AsDouble:
		value = v.AsDouble
	case *metricpb.NumberDataPoint_AsInt:
		value = float64(v.AsInt)
	}

	ts := time.Unix(0, int64(dp.GetTimeUnixNano()))
	if dp.GetTimeUnixNano() == 0 {
		ts = time.Now()
	}

	pointTags := mergeTags(resourceTags, attributesToTags(dp.GetAttributes()))
	pointTags = append(pointTags, tags.Tag{Key: "metric_type", Value: otelmapper.MetricTypeOTLPIngest})
	// Carry the inbound instrument type and unit so the mapper re-exports
	// a counter as a counter (not a gauge) and keeps the unit, instead of
	// flattening every ingested metric to a unitless gauge. "otel_type"
	// and "unit" are the same control tags the mapper and the OTLP/HTTP
	// sinks already read.
	pointTags = append(pointTags, tags.Tag{Key: "otel_type", Value: otelType})
	if unit != "" {
		pointTags = append(pointTags, tags.Tag{Key: "unit", Value: unit})
	}

	return data_store.DataPoint{
		Name:      name,
		Timestamp: ts,
		Value:     value,
		Tags:      pointTags,
	}
}

// mergeTags returns resource tags overlaid with point tags. On a key
// collision the point (more specific) tag wins.
func mergeTags(resourceTags, pointTags []tags.Tag) []tags.Tag {
	if len(resourceTags) == 0 {
		return append([]tags.Tag{}, pointTags...)
	}
	pointKeys := make(map[string]bool, len(pointTags))
	for _, t := range pointTags {
		pointKeys[t.Key] = true
	}
	merged := make([]tags.Tag, 0, len(resourceTags)+len(pointTags))
	for _, t := range resourceTags {
		if !pointKeys[t.Key] {
			merged = append(merged, t)
		}
	}
	merged = append(merged, pointTags...)
	return merged
}

func attributesToTags(attrs []*commonpb.KeyValue) []tags.Tag {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]tags.Tag, 0, len(attrs))
	for _, kv := range attrs {
		if kv.GetKey() == "" {
			continue
		}
		out = append(out, tags.Tag{Key: kv.GetKey(), Value: anyValueToString(kv.GetValue())})
	}
	return out
}

func anyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("%t", val.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("%d", val.IntValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("%g", val.DoubleValue)
	case *commonpb.AnyValue_BytesValue:
		return string(val.BytesValue)
	default:
		return ""
	}
}
