package otlpreceiver

import (
	"fmt"
	"strconv"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
)

// flattenResourceMetrics converts the OTLP wire form of a metrics
// payload into the agent's flat DataPoint slice. Every OTLP metric
// family is mapped: Gauge and Sum become a scalar DataPoint each;
// Histogram, ExponentialHistogram and Summary are expanded into their
// Prometheus classic-histogram component series (_count, _sum,
// _bucket{le}, {quantile}, _min/_max) so they flow through the same
// scalar DataPoint path. Only a genuinely unrecognized/unset data type
// is reported via the returned dropped count.
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
	case *metricpb.Metric_Histogram:
		for _, dp := range data.Histogram.GetDataPoints() {
			points = append(points, histogramToDataPoints(name, unit, dp, resourceTags)...)
		}
	case *metricpb.Metric_ExponentialHistogram:
		for _, dp := range data.ExponentialHistogram.GetDataPoints() {
			points = append(points, expHistogramToDataPoints(name, unit, dp, resourceTags)...)
		}
	case *metricpb.Metric_Summary:
		for _, dp := range data.Summary.GetDataPoints() {
			points = append(points, summaryToDataPoints(name, unit, dp, resourceTags)...)
		}
	default:
		// Unset or a metric family newer than this proto vendoring: no
		// data to map. Count it so the caller can surface a partial
		// success instead of pretending the export was lossless.
		dropped++
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

func numberPointToDataPoint(name, unit, otelType string, dp *metricpb.NumberDataPoint, resourceTags []tags.Tag) data_store.DataPoint {
	var value float64
	switch v := dp.GetValue().(type) {
	case *metricpb.NumberDataPoint_AsDouble:
		value = v.AsDouble
	case *metricpb.NumberDataPoint_AsInt:
		value = float64(v.AsInt)
	}

	return data_store.DataPoint{
		Name:      name,
		Timestamp: pointTime(dp.GetTimeUnixNano()),
		Value:     value,
		Tags:      ingestTags(unit, otelType, dp.GetAttributes(), resourceTags),
	}
}

// pointTime converts an OTLP nanosecond timestamp to time.Time, defaulting
// to now when the sender left it unset (0).
func pointTime(tsNano uint64) time.Time {
	if tsNano == 0 {
		return time.Now()
	}
	return time.Unix(0, int64(tsNano))
}

// ingestTags builds the standard otlp_ingest tag set for one emitted point:
// resource tags overlaid with the point attributes, plus the control tags
// the mapper and the OTLP/HTTP sinks read — metric_type=otlp_ingest, the
// inbound otel_type (so a counter re-exports as a counter, not a gauge) and
// unit — plus any per-series discriminator (le, quantile).
func ingestTags(unit, otelType string, attrs []*commonpb.KeyValue, resourceTags []tags.Tag, extra ...tags.Tag) []tags.Tag {
	out := mergeTags(resourceTags, attributesToTags(attrs))
	out = append(out, tags.Tag{Key: "metric_type", Value: otelmapper.MetricTypeOTLPIngest})
	out = append(out, tags.Tag{Key: "otel_type", Value: otelType})
	if unit != "" {
		out = append(out, tags.Tag{Key: "unit", Value: unit})
	}
	out = append(out, extra...)
	return out
}

// histogramToDataPoints expands one OTLP explicit-bucket histogram point
// into the Prometheus classic-histogram scalar series: <name>_count,
// <name>_sum (when present), cumulative <name>_bucket{le=<bound>} (plus a
// terminal le="+Inf"), and optional <name>_min / <name>_max gauges. OTLP
// bucket_counts are per-bucket; Prometheus buckets are cumulative, so the
// counts are accumulated. Count/bucket series are dimensionless ("1"); the
// value-bearing sum/min/max keep the metric's unit.
func histogramToDataPoints(name, unit string, dp *metricpb.HistogramDataPoint, resourceTags []tags.Tag) []data_store.DataPoint {
	ts := pointTime(dp.GetTimeUnixNano())
	attrs := dp.GetAttributes()

	out := []data_store.DataPoint{{
		Name:      name + "_count",
		Timestamp: ts,
		Value:     float64(dp.GetCount()),
		Tags:      ingestTags("1", "counter", attrs, resourceTags),
	}}
	if dp.Sum != nil {
		out = append(out, data_store.DataPoint{
			Name: name + "_sum", Timestamp: ts, Value: dp.GetSum(),
			Tags: ingestTags(unit, "counter", attrs, resourceTags),
		})
	}

	bounds := dp.GetExplicitBounds()
	var cumulative uint64
	for i, bc := range dp.GetBucketCounts() {
		cumulative += bc
		le := "+Inf"
		if i < len(bounds) {
			le = strconv.FormatFloat(bounds[i], 'g', -1, 64)
		}
		out = append(out, data_store.DataPoint{
			Name: name + "_bucket", Timestamp: ts, Value: float64(cumulative),
			Tags: ingestTags("1", "counter", attrs, resourceTags, tags.Tag{Key: "le", Value: le}),
		})
	}

	out = append(out, minMaxPoints(name, unit, ts, attrs, resourceTags, dp.Min, dp.Max)...)
	return out
}

// expHistogramToDataPoints maps one OTLP exponential-histogram point to its
// scalar aggregates (_count, _sum, _min, _max). The base-2 bucket expansion
// is deferred (#659) — exponential buckets have no fixed le set — so only
// the aggregates are emitted here.
func expHistogramToDataPoints(name, unit string, dp *metricpb.ExponentialHistogramDataPoint, resourceTags []tags.Tag) []data_store.DataPoint {
	ts := pointTime(dp.GetTimeUnixNano())
	attrs := dp.GetAttributes()

	out := []data_store.DataPoint{{
		Name:      name + "_count",
		Timestamp: ts,
		Value:     float64(dp.GetCount()),
		Tags:      ingestTags("1", "counter", attrs, resourceTags),
	}}
	if dp.Sum != nil {
		out = append(out, data_store.DataPoint{
			Name: name + "_sum", Timestamp: ts, Value: dp.GetSum(),
			Tags: ingestTags(unit, "counter", attrs, resourceTags),
		})
	}
	out = append(out, minMaxPoints(name, unit, ts, attrs, resourceTags, dp.Min, dp.Max)...)
	return out
}

// summaryToDataPoints expands one OTLP summary point into <name>_count,
// <name>_sum and one <name>{quantile=q} gauge per reported quantile.
func summaryToDataPoints(name, unit string, dp *metricpb.SummaryDataPoint, resourceTags []tags.Tag) []data_store.DataPoint {
	ts := pointTime(dp.GetTimeUnixNano())
	attrs := dp.GetAttributes()

	out := []data_store.DataPoint{
		{
			Name: name + "_count", Timestamp: ts, Value: float64(dp.GetCount()),
			Tags: ingestTags("1", "counter", attrs, resourceTags),
		},
		{
			Name: name + "_sum", Timestamp: ts, Value: dp.GetSum(),
			Tags: ingestTags(unit, "counter", attrs, resourceTags),
		},
	}
	for _, qv := range dp.GetQuantileValues() {
		q := strconv.FormatFloat(qv.GetQuantile(), 'g', -1, 64)
		out = append(out, data_store.DataPoint{
			Name: name, Timestamp: ts, Value: qv.GetValue(),
			Tags: ingestTags(unit, "gauge", attrs, resourceTags, tags.Tag{Key: "quantile", Value: q}),
		})
	}
	return out
}

// minMaxPoints emits optional _min / _max gauges when the histogram point
// carries them (both are optional OTLP fields).
func minMaxPoints(name, unit string, ts time.Time, attrs []*commonpb.KeyValue, resourceTags []tags.Tag, min, max *float64) []data_store.DataPoint {
	var out []data_store.DataPoint
	if min != nil {
		out = append(out, data_store.DataPoint{
			Name: name + "_min", Timestamp: ts, Value: *min,
			Tags: ingestTags(unit, "gauge", attrs, resourceTags),
		})
	}
	if max != nil {
		out = append(out, data_store.DataPoint{
			Name: name + "_max", Timestamp: ts, Value: *max,
			Tags: ingestTags(unit, "gauge", attrs, resourceTags),
		})
	}
	return out
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
