package otlpreceiver

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
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

func TestFlatten_HistogramDropped(t *testing.T) {
	hist := &metricpb.Metric{
		Name: "http.server.duration",
		Data: &metricpb.Metric_Histogram{Histogram: &metricpb.Histogram{
			DataPoints: []*metricpb.HistogramDataPoint{{}, {}},
		}},
	}
	rm := wrap(nil, hist, gaugeMetric("up", 1))

	points, dropped := flattenResourceMetrics(rm)

	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
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
