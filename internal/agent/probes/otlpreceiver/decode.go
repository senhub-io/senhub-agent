package otlpreceiver

import (
	"fmt"
	"math"
	"strconv"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// flattenResourceMetrics converts the OTLP wire form of a metrics
// payload into the agent's flat DataPoint slice. Every OTLP metric
// family is mapped: Gauge and Sum become a scalar DataPoint each;
// an explicit-bucket Histogram becomes ONE DataPoint carrying the
// native histogram payload (re-exported natively over OTLP and as a
// Prometheus classic histogram, while Value holds the count for
// scalar-only sinks); ExponentialHistogram and Summary are expanded
// into their scalar component series (_count, _sum, {quantile},
// _min/_max) as before. Only a genuinely unrecognized/unset data type
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
		extra := temporalityTags(data.Sum.GetAggregationTemporality())
		for _, dp := range data.Sum.GetDataPoints() {
			points = append(points, numberPointToDataPoint(name, unit, otelType, dp, resourceTags, extra...))
		}
	case *metricpb.Metric_Histogram:
		extra := temporalityTags(data.Histogram.GetAggregationTemporality())
		for _, dp := range data.Histogram.GetDataPoints() {
			if !histogramDataPointValid(dp) {
				// Malformed bucket layout — dropping here keeps a poison
				// point out of the OTLP export store, where a downstream
				// collector could reject the whole batch on every push.
				dropped++
				continue
			}
			points = append(points, histogramToDataPoint(name, unit, dp, resourceTags, extra...))
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
// OTel instrument type: monotonic → counter, non-monotonic →
// updowncounter. Temporality is carried separately by the
// otel_temporality control tag (see temporalityTags), so a delta sum
// keeps its counter semantics and re-exports as a delta Sum instead of
// degrading to a gauge (#661).
func sumInstrumentType(sum *metricpb.Sum) string {
	if sum.GetIsMonotonic() {
		return "counter"
	}
	return "updowncounter"
}

// temporalityTags returns the otel_temporality control tag for a
// delta-temporality sum or histogram, and nil otherwise. Cumulative is
// the pipeline-wide default, so it is deliberately NOT tagged — an
// unspecified/unknown temporality then falls back to cumulative
// everywhere downstream, preserving pre-#661 behavior for every
// non-delta sender.
func temporalityTags(t metricpb.AggregationTemporality) []tags.Tag {
	if t == metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
		return []tags.Tag{{Key: otelmapper.TemporalityTagKey, Value: otelmapper.TemporalityDelta}}
	}
	return nil
}

func numberPointToDataPoint(name, unit, otelType string, dp *metricpb.NumberDataPoint, resourceTags []tags.Tag, extra ...tags.Tag) data_store.DataPoint {
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
		Tags:      ingestTags(unit, otelType, dp.GetAttributes(), resourceTags, extra...),
	}
}

// pointTime converts an OTLP nanosecond timestamp to time.Time, defaulting
// to now when the sender left it unset (0).
func pointTime(tsNano uint64) time.Time {
	// Unset (0) or a value past what int64 nanoseconds can hold (hostile or
	// buggy sender — a real timestamp stays under this until year 2262)
	// falls back to now instead of wrapping to a negative (year ~1677).
	if tsNano == 0 || tsNano > math.MaxInt64 {
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

// histogramToDataPoint maps one OTLP explicit-bucket histogram point to a
// single DataPoint carrying the native histogram payload. Value is the
// observation count — the scalar fallback for sinks without histogram
// rendering (PRTG, Nagios, cloud) — while histogram-aware sinks (OTLP
// push, Prometheus exposition) re-export the full distribution from the
// payload. Slices and optional pointers are copied so the stored payload
// never aliases the decoded proto message.
func histogramToDataPoint(name, unit string, dp *metricpb.HistogramDataPoint, resourceTags []tags.Tag, extra ...tags.Tag) data_store.DataPoint {
	h := &datapoint.HistogramValue{
		Count:          dp.GetCount(),
		Sum:            copyOptionalFloat(dp.Sum),
		Min:            copyOptionalFloat(dp.Min),
		Max:            copyOptionalFloat(dp.Max),
		BucketCounts:   append([]uint64(nil), dp.GetBucketCounts()...),
		ExplicitBounds: append([]float64(nil), dp.GetExplicitBounds()...),
	}
	return data_store.DataPoint{
		Name:      name,
		Timestamp: pointTime(dp.GetTimeUnixNano()),
		Value:     float64(dp.GetCount()),
		// Strip a sender-supplied `le`: it is reserved for the histogram
		// bucket ladder. Dropping it at decode keeps it off BOTH the
		// Prometheus exposition (where a duplicate broke the whole page)
		// AND the OTLP export (where it would otherwise pollute the
		// downstream backend with a bogus `le` dimension).
		Tags:      ingestTags(unit, "histogram", withoutAttr(dp.GetAttributes(), "le"), resourceTags, extra...),
		Histogram: h,
	}
}

func copyOptionalFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// histogramDataPointValid enforces the OTLP explicit-bucket invariants: when
// bucket counts are present there must be exactly one more of them than
// explicit bounds, and the bucket counts must not total more than the
// point's Count. An empty bucket layout (count/sum only) is valid, and so
// is sum(BucketCounts) < Count (the remainder is representable in the
// implicit +Inf bucket, which serializers write as Count). But a hostile
// sum(BucketCounts) > Count is unrepresentable: the classic Prometheus
// exposition would render a NON-monotone bucket ladder (a cumulative
// bucket above the +Inf value), which corrupts histogram_quantile
// downstream. A point that violates either invariant is malformed and
// must not reach the native export.
func histogramDataPointValid(dp *metricpb.HistogramDataPoint) bool {
	counts := dp.GetBucketCounts()
	if len(counts) == 0 {
		return true
	}
	if len(counts) != len(dp.GetExplicitBounds())+1 {
		return false
	}
	var sum uint64
	for _, c := range counts {
		if sum+c < sum { // uint64 overflow: hostile by construction
			return false
		}
		sum += c
	}
	return sum <= dp.GetCount()
}

// withoutAttr returns attrs minus any entry with the given key, without
// mutating the input (which aliases the received proto message). Used to
// drop Prometheus-reserved keys (`le`, `quantile`) a sender may have put on
// a histogram/summary before they become tags.
func withoutAttr(attrs []*commonpb.KeyValue, key string) []*commonpb.KeyValue {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			out := make([]*commonpb.KeyValue, 0, len(attrs)-1)
			for _, k := range attrs {
				if k.GetKey() != key {
					out = append(out, k)
				}
			}
			return out
		}
	}
	return attrs
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
	// `quantile` is reserved for the summary's quantile series; strip any
	// sender-supplied one so it cannot collide with the generated tags.
	attrs := withoutAttr(dp.GetAttributes(), "quantile")

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
