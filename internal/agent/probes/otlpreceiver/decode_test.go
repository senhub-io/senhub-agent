package otlpreceiver

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/tags"
)

func strAttr(key, val string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: val}},
	}
}

func gaugeMetric(name string, value float64, attrs ...*commonpb.KeyValue) *metricpb.Metric {
	return &metricpb.Metric{
		Name: name,
		Data: &metricpb.Metric_Gauge{Gauge: &metricpb.Gauge{
			DataPoints: []*metricpb.NumberDataPoint{
				{
					Attributes:   attrs,
					TimeUnixNano: 1_700_000_000_000_000_000,
					Value:        &metricpb.NumberDataPoint_AsDouble{AsDouble: value},
				},
			},
		}},
	}
}

func sumMetric(name string, value int64) *metricpb.Metric {
	return &metricpb.Metric{
		Name: name,
		Data: &metricpb.Metric_Sum{Sum: &metricpb.Sum{
			DataPoints: []*metricpb.NumberDataPoint{
				{
					TimeUnixNano: 1_700_000_000_000_000_000,
					Value:        &metricpb.NumberDataPoint_AsInt{AsInt: value},
				},
			},
		}},
	}
}

func wrap(resourceAttrs []*commonpb.KeyValue, metrics ...*metricpb.Metric) []*metricpb.ResourceMetrics {
	return []*metricpb.ResourceMetrics{
		{
			Resource: &resourcepb.Resource{Attributes: resourceAttrs},
			ScopeMetrics: []*metricpb.ScopeMetrics{
				{Metrics: metrics},
			},
		},
	}
}

func TestFlatten_GaugeAndSum(t *testing.T) {
	rm := wrap(nil, gaugeMetric("system.cpu.utilization", 0.42), sumMetric("http.server.requests", 7))

	points, dropped := flattenResourceMetrics(rm)

	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}

	byName := map[string]float64{}
	for _, p := range points {
		byName[p.Name] = p.Value
	}
	if got := byName["system.cpu.utilization"]; got != 0.42 {
		t.Errorf("gauge value = %v, want 0.42", got)
	}
	if got := byName["http.server.requests"]; got != 7 {
		t.Errorf("sum value = %v, want 7", got)
	}
}

func float64Ptr(v float64) *float64 { return &v }

// tagMapOf indexes a point's tags by key for assertions.
func tagMapOf(tgs []tags.Tag) map[string]string {
	m := map[string]string{}
	for _, tg := range tgs {
		m[tg.Key] = tg.Value
	}
	return m
}

func TestFlatten_HistogramNativePayload(t *testing.T) {
	// 3 bucket counts over 2 explicit bounds => buckets (-inf,0.1], (0.1,0.5], (0.5,+Inf].
	hist := &metricpb.Metric{
		Name: "http.server.duration", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{
				TimeUnixNano:   1_700_000_000_000_000_000,
				Count:          6,
				Sum:            float64Ptr(4.2),
				BucketCounts:   []uint64{1, 2, 3},
				ExplicitBounds: []float64{0.1, 0.5},
				Min:            float64Ptr(0.05),
				Max:            float64Ptr(0.9),
			}},
		}},
	}
	points, dropped := flattenResourceMetrics(wrap(nil, hist))
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1 (native histogram, no scalar expansion)", len(points))
	}

	p := points[0]
	if p.Name != "http.server.duration" {
		t.Errorf("name = %q, want bare metric name", p.Name)
	}
	if p.Value != 6 {
		t.Errorf("Value = %v, want 6 (observation count as scalar fallback)", p.Value)
	}
	tm := tagMapOf(p.Tags)
	if tm["otel_type"] != "histogram" {
		t.Errorf("otel_type = %q, want histogram", tm["otel_type"])
	}
	if tm["unit"] != "s" {
		t.Errorf("unit = %q, want s", tm["unit"])
	}

	h := p.Histogram
	if h == nil {
		t.Fatal("Histogram payload is nil")
	}
	if h.Count != 6 {
		t.Errorf("payload Count = %d, want 6", h.Count)
	}
	if h.Sum == nil || *h.Sum != 4.2 {
		t.Errorf("payload Sum = %v, want 4.2", h.Sum)
	}
	if h.Min == nil || *h.Min != 0.05 {
		t.Errorf("payload Min = %v, want 0.05", h.Min)
	}
	if h.Max == nil || *h.Max != 0.9 {
		t.Errorf("payload Max = %v, want 0.9", h.Max)
	}
	if len(h.BucketCounts) != 3 || h.BucketCounts[0] != 1 || h.BucketCounts[1] != 2 || h.BucketCounts[2] != 3 {
		t.Errorf("payload BucketCounts = %v, want [1 2 3] (per-bucket, non-cumulative)", h.BucketCounts)
	}
	if len(h.ExplicitBounds) != 2 || h.ExplicitBounds[0] != 0.1 || h.ExplicitBounds[1] != 0.5 {
		t.Errorf("payload ExplicitBounds = %v, want [0.1 0.5]", h.ExplicitBounds)
	}
}

func TestFlatten_HistogramOptionalFieldsAbsent(t *testing.T) {
	hist := &metricpb.Metric{
		Name: "h", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{
				TimeUnixNano:   1_700_000_000_000_000_000,
				Count:          2,
				BucketCounts:   []uint64{2},
				ExplicitBounds: nil,
			}},
		}},
	}
	points, _ := flattenResourceMetrics(wrap(nil, hist))
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	h := points[0].Histogram
	if h == nil {
		t.Fatal("Histogram payload is nil")
	}
	if h.Sum != nil || h.Min != nil || h.Max != nil {
		t.Errorf("optional Sum/Min/Max must stay nil when absent from the wire: %+v", h)
	}
}

func TestFlatten_MalformedHistogramDropped(t *testing.T) {
	// BucketCounts (2) with no explicit bounds violates len==bounds+1 (=1)
	// → malformed → dropped, so a poison point never reaches the OTLP export.
	bad := &metricpb.Metric{
		Name: "bad.hist", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{
				Count: 5, BucketCounts: []uint64{2, 3}, ExplicitBounds: nil,
			}},
		}},
	}
	// A count-only histogram (no bucket layout) is valid and kept.
	countOnly := &metricpb.Metric{
		Name: "ok.hist", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{Count: 3}},
		}},
	}
	points, dropped := flattenResourceMetrics(wrap(nil, bad, countOnly))
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (malformed histogram)", dropped)
	}
	if len(points) != 1 || points[0].Name != "ok.hist" {
		t.Errorf("points = %+v, want a single ok.hist point", points)
	}
	if points[0].Histogram == nil || points[0].Histogram.Count != 3 {
		t.Errorf("count-only histogram payload = %+v, want Count=3", points[0].Histogram)
	}
}

func TestFlatten_HistogramStripsReservedLe(t *testing.T) {
	hist := &metricpb.Metric{
		Name: "h", Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{
				Attributes:     []*commonpb.KeyValue{strAttr("le", "sneaky"), strAttr("route", "/x")},
				Count:          6,
				BucketCounts:   []uint64{1, 2, 3},
				ExplicitBounds: []float64{0.1, 0.5},
			}},
		}},
	}
	points, _ := flattenResourceMetrics(wrap(nil, hist))
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	tm := tagMapOf(points[0].Tags)
	if _, ok := tm["le"]; ok {
		// A reserved `le` must never survive onto a histogram — it would
		// pollute the OTLP export and break the Prometheus exposition.
		t.Errorf("reserved le must be stripped, got le=%q", tm["le"])
	}
	if tm["route"] != "/x" {
		t.Errorf("non-reserved attr must survive, tags=%v", tm)
	}
}

func TestFlatten_SummaryExpanded(t *testing.T) {
	summary := &metricpb.Metric{
		Name: "rpc.latency", Unit: "ms",
		Data: &metricpb.Metric_Summary{Summary: &metricpb.Summary{
			DataPoints: []*metricpb.SummaryDataPoint{{
				TimeUnixNano: 1_700_000_000_000_000_000,
				Count:        10,
				Sum:          55,
				QuantileValues: []*metricpb.SummaryDataPoint_ValueAtQuantile{
					{Quantile: 0.5, Value: 2},
					{Quantile: 0.99, Value: 9},
				},
			}},
		}},
	}
	points, dropped := flattenResourceMetrics(wrap(nil, summary))
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}

	byName := map[string]float64{}
	quantiles := map[string]float64{}
	for _, p := range points {
		tm := tagMapOf(p.Tags)
		if q, ok := tm["quantile"]; ok {
			quantiles[q] = p.Value
			if p.Name != "rpc.latency" {
				t.Errorf("quantile series name = %q, want bare metric name", p.Name)
			}
			if tm["otel_type"] != "gauge" {
				t.Errorf("quantile otel_type = %q, want gauge", tm["otel_type"])
			}
			continue
		}
		byName[p.Name] = p.Value
	}
	if byName["rpc.latency_count"] != 10 || byName["rpc.latency_sum"] != 55 {
		t.Errorf("_count/_sum = %v/%v, want 10/55", byName["rpc.latency_count"], byName["rpc.latency_sum"])
	}
	if quantiles["0.5"] != 2 || quantiles["0.99"] != 9 {
		t.Errorf("quantiles = %v, want 0.5=2 0.99=9", quantiles)
	}
}

func TestFlatten_ExpHistogramAggregatesOnly(t *testing.T) {
	eh := &metricpb.Metric{
		Name: "db.query.duration", Unit: "s",
		Data: &metricpb.Metric_ExponentialHistogram{ExponentialHistogram: &metricpb.ExponentialHistogram{
			DataPoints: []*metricpb.ExponentialHistogramDataPoint{{
				TimeUnixNano: 1_700_000_000_000_000_000,
				Count:        8,
				Sum:          float64Ptr(3.3),
				Min:          float64Ptr(0.01),
				Max:          float64Ptr(1.2),
			}},
		}},
	}
	points, dropped := flattenResourceMetrics(wrap(nil, eh))
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}

	byName := map[string]float64{}
	for _, p := range points {
		byName[p.Name] = p.Value
		if p.Name == "db.query.duration_bucket" {
			t.Errorf("exp-histogram should not emit bucket series (deferred), got %+v", p)
		}
	}
	if byName["db.query.duration_count"] != 8 || byName["db.query.duration_sum"] != 3.3 {
		t.Errorf("_count/_sum = %v/%v, want 8/3.3", byName["db.query.duration_count"], byName["db.query.duration_sum"])
	}
	if byName["db.query.duration_min"] != 0.01 || byName["db.query.duration_max"] != 1.2 {
		t.Errorf("_min/_max = %v/%v, want 0.01/1.2", byName["db.query.duration_min"], byName["db.query.duration_max"])
	}
}

func TestFlatten_UnsetDataTypeDropped(t *testing.T) {
	// A metric with no Data oneof set is unmappable and must be counted.
	rm := wrap(nil, &metricpb.Metric{Name: "mystery"}, gaugeMetric("up", 1))
	points, dropped := flattenResourceMetrics(rm)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(points) != 1 || points[0].Name != "up" {
		t.Errorf("points = %+v, want a single 'up' point", points)
	}
}

func TestFlatten_ResourceAttributesFoldedAsTags(t *testing.T) {
	rm := wrap(
		[]*commonpb.KeyValue{strAttr("host.name", "edge-01"), strAttr("service.name", "router")},
		gaugeMetric("queue.depth", 3, strAttr("queue", "ingress")),
	)

	points, _ := flattenResourceMetrics(rm)
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}

	tagMap := map[string]string{}
	for _, tg := range points[0].Tags {
		tagMap[tg.Key] = tg.Value
	}
	if tagMap["host.name"] != "edge-01" {
		t.Errorf("missing resource tag host.name: %v", tagMap)
	}
	if tagMap["service.name"] != "router" {
		t.Errorf("missing resource tag service.name: %v", tagMap)
	}
	if tagMap["queue"] != "ingress" {
		t.Errorf("missing datapoint tag queue: %v", tagMap)
	}
	if tagMap["metric_type"] != "otlp_ingest" {
		t.Errorf("metric_type tag = %q, want otlp_ingest", tagMap["metric_type"])
	}
}

func TestFlatten_DatapointAttributeWinsOverResource(t *testing.T) {
	rm := wrap(
		[]*commonpb.KeyValue{strAttr("env", "resource-level")},
		gaugeMetric("m", 1, strAttr("env", "point-level")),
	)

	points, _ := flattenResourceMetrics(rm)
	count := 0
	var got string
	for _, tg := range points[0].Tags {
		if tg.Key == "env" {
			count++
			got = tg.Value
		}
	}
	if count != 1 {
		t.Fatalf("env tag appears %d times, want 1 (point should override resource)", count)
	}
	if got != "point-level" {
		t.Errorf("env = %q, want point-level", got)
	}
}

func sumMetricFull(name, unit string, monotonic bool, temporality metricpb.AggregationTemporality) *metricpb.Metric {
	return &metricpb.Metric{
		Name: name,
		Unit: unit,
		Data: &metricpb.Metric_Sum{Sum: &metricpb.Sum{
			IsMonotonic:            monotonic,
			AggregationTemporality: temporality,
			DataPoints: []*metricpb.NumberDataPoint{
				{
					TimeUnixNano: 1_700_000_000_000_000_000,
					Value:        &metricpb.NumberDataPoint_AsInt{AsInt: 5},
				},
			},
		}},
	}
}

// TestFlatten_PreservesInstrumentTypeAndUnit pins the OTLP re-export fix:
// before it, every ingested metric was flattened to a unitless value with
// no instrument type, so the mapper re-exported counters as gauges and
// dropped the unit. The decoder now stamps otel_type and unit control
// tags. A monotonic sum is a counter and a non-monotonic one an
// updowncounter regardless of temporality (#661 — delta rides the
// separate otel_temporality tag instead of degrading the type to gauge).
func TestFlatten_PreservesInstrumentTypeAndUnit(t *testing.T) {
	cumulative := metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
	delta := metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA

	cases := []struct {
		name         string
		metric       *metricpb.Metric
		wantType     string
		wantUnit     string
		wantHaveUnit bool
	}{
		{"gauge", gaugeMetric("system.cpu.utilization", 0.42), "gauge", "", false},
		{"cumulative monotonic sum → counter", sumMetricFull("http.server.requests", "{request}", true, cumulative), "counter", "{request}", true},
		{"cumulative non-monotonic sum → updowncounter", sumMetricFull("queue.depth", "{item}", false, cumulative), "updowncounter", "{item}", true},
		{"delta monotonic sum stays counter", sumMetricFull("http.server.duration", "s", true, delta), "counter", "s", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			points, dropped := flattenResourceMetrics(wrap(nil, tc.metric))
			if dropped != 0 {
				t.Fatalf("dropped = %d, want 0", dropped)
			}
			if len(points) != 1 {
				t.Fatalf("got %d points, want 1", len(points))
			}
			tagMap := map[string]string{}
			haveUnit := false
			for _, tg := range points[0].Tags {
				tagMap[tg.Key] = tg.Value
				if tg.Key == "unit" {
					haveUnit = true
				}
			}
			if tagMap["otel_type"] != tc.wantType {
				t.Errorf("otel_type = %q, want %q", tagMap["otel_type"], tc.wantType)
			}
			if haveUnit != tc.wantHaveUnit {
				t.Errorf("unit tag present = %v, want %v (tags=%v)", haveUnit, tc.wantHaveUnit, tagMap)
			}
			if tc.wantHaveUnit && tagMap["unit"] != tc.wantUnit {
				t.Errorf("unit = %q, want %q", tagMap["unit"], tc.wantUnit)
			}
		})
	}
}

// histMetricWithTemporality builds a minimal valid explicit-bucket
// histogram carrying the given aggregation temporality.
func histMetricWithTemporality(name string, temporality metricpb.AggregationTemporality) *metricpb.Metric {
	return &metricpb.Metric{
		Name: name, Unit: "s",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			AggregationTemporality: temporality,
			DataPoints: []*metricpb.HistogramDataPoint{{
				TimeUnixNano:   1_700_000_000_000_000_000,
				Count:          3,
				BucketCounts:   []uint64{2, 1},
				ExplicitBounds: []float64{0.5},
			}},
		}},
	}
}

// TestFlatten_DeltaTemporalityThreaded pins the #661 contract at the
// decode boundary: only an explicit DELTA temporality produces the
// otel_temporality control tag; cumulative and unspecified sums and
// histograms stay untagged so everything downstream keeps its
// cumulative default.
func TestFlatten_DeltaTemporalityThreaded(t *testing.T) {
	unspecified := metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED
	cumulative := metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
	delta := metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA

	cases := []struct {
		name      string
		metric    *metricpb.Metric
		wantDelta bool
	}{
		{"delta sum tagged", sumMetricFull("s.delta", "s", true, delta), true},
		{"cumulative sum untagged", sumMetricFull("s.cumulative", "s", true, cumulative), false},
		{"unspecified sum untagged", sumMetricFull("s.unspecified", "s", true, unspecified), false},
		{"delta histogram tagged", histMetricWithTemporality("h.delta", delta), true},
		{"cumulative histogram untagged", histMetricWithTemporality("h.cumulative", cumulative), false},
		{"unspecified histogram untagged", histMetricWithTemporality("h.unspecified", unspecified), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			points, dropped := flattenResourceMetrics(wrap(nil, tc.metric))
			if dropped != 0 {
				t.Fatalf("dropped = %d, want 0", dropped)
			}
			if len(points) != 1 {
				t.Fatalf("got %d points, want 1", len(points))
			}
			tm := tagMapOf(points[0].Tags)
			got, tagged := tm[otelmapper.TemporalityTagKey]
			if tagged != tc.wantDelta {
				t.Fatalf("otel_temporality tag present = %v, want %v (tags=%v)", tagged, tc.wantDelta, tm)
			}
			if tc.wantDelta && got != otelmapper.TemporalityDelta {
				t.Errorf("otel_temporality = %q, want %q", got, otelmapper.TemporalityDelta)
			}
		})
	}
}

func TestAnyValueToString(t *testing.T) {
	cases := []struct {
		name string
		in   *commonpb.AnyValue
		want string
	}{
		{"string", &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "x"}}, "x"},
		{"bool", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}, "true"},
		{"int", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}, "42"},
		{"double", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 1.5}}, "1.5"},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anyValueToString(c.in); got != c.want {
				t.Errorf("anyValueToString = %q, want %q", got, c.want)
			}
		})
	}
}
