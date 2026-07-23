package prometheus

import (
	"bytes"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// newTextParser returns a TextParser initialized with LegacyValidation,
// which matches the classic Prometheus naming rules that OTelNameToPromName
// produces (no UTF-8 in metric/label names).
func newTextParser() expfmt.TextParser {
	return expfmt.NewTextParser(model.LegacyValidation)
}

// TestSerialize_ParsesWithExpfmt is the critical round-trip test: anything
// we emit MUST parse with the canonical Prometheus text parser.
func TestSerialize_ParsesWithExpfmt(t *testing.T) {
	records := []otelmapper.OtelRecord{
		{
			Name:        "system.cpu.utilization",
			Unit:        "1",
			Type:        "gauge",
			Attributes:  map[string]string{"probe_name": "cpu-1", "probe_type": "cpu", "cpu.mode": "user"},
			Value:       0.42,
			Description: "Overall CPU utilization",
		},
		{
			Name:        "system.cpu.time",
			Unit:        "s",
			Type:        "counter",
			Attributes:  map[string]string{"probe_name": "cpu-1", "cpu.mode": "user"},
			Value:       1234.5,
			Description: "Time spent in user mode",
		},
		{
			Name:        "hw.status",
			Unit:        "1",
			Type:        "updowncounter",
			Attributes:  map[string]string{"probe_name": "redfish-1", "hw.type": "physical_disk", "hw.id": "disk.1", "hw.state": "ok"},
			Value:       1,
			Description: "Drive health status",
		},
		{
			Name:        "hw.status",
			Unit:        "1",
			Type:        "updowncounter",
			Attributes:  map[string]string{"probe_name": "redfish-1", "hw.type": "physical_disk", "hw.id": "disk.1", "hw.state": "failed"},
			Value:       0,
			Description: "Drive health status",
		},
	}

	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}

	body := buf.String()
	t.Logf("Output:\n%s", body)

	p := newTextParser()
	parsed, err := p.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("expfmt parse failed: %v\nbody:\n%s", err, body)
	}

	if _, ok := parsed["senhub_system_cpu_utilization_ratio"]; !ok {
		t.Errorf("expected senhub_system_cpu_utilization_ratio in parsed output; got keys: %v", keys(parsed))
	}
	if _, ok := parsed["senhub_system_cpu_time_seconds_total"]; !ok {
		t.Errorf("expected senhub_system_cpu_time_seconds_total in parsed output; got keys: %v", keys(parsed))
	}
	if _, ok := parsed["senhub_hw_status"]; !ok {
		t.Errorf("expected senhub_hw_status in parsed output; got keys: %v", keys(parsed))
	}
}

func TestSerialize_GroupsSameMetricName(t *testing.T) {
	// Two OTel records that collapse to the same Prometheus name should
	// share a single HELP/TYPE header.
	records := []otelmapper.OtelRecord{
		{
			Name:       "system.cpu.time",
			Unit:       "s",
			Type:       "counter",
			Attributes: map[string]string{"cpu.mode": "user"},
			Value:      100,
		},
		{
			Name:       "system.cpu.time",
			Unit:       "s",
			Type:       "counter",
			Attributes: map[string]string{"cpu.mode": "system"},
			Value:      50,
		},
	}
	var buf bytes.Buffer
	_ = SerializeToTextExposition(records, &buf, SerializeOptions{})
	body := buf.String()

	typeCount := strings.Count(body, "# TYPE senhub_system_cpu_time_seconds_total")
	if typeCount != 1 {
		t.Errorf("expected 1 TYPE line for grouped metric, got %d.\nbody:\n%s", typeCount, body)
	}
}

func TestSerialize_LabelsEscaped(t *testing.T) {
	records := []otelmapper.OtelRecord{
		{
			Name:       "test.metric",
			Unit:       "1",
			Type:       "gauge",
			Attributes: map[string]string{"message": `hello "world"\n`},
			Value:      1,
		},
	}
	var buf bytes.Buffer
	_ = SerializeToTextExposition(records, &buf, SerializeOptions{})

	p := newTextParser()
	_, err := p.TextToMetricFamilies(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("parse failed with escaped label: %v\nbody:\n%s", err, buf.String())
	}
}

// TestSerialize_HistogramWithReservedLeAttribute pins the C1 fix: a
// sender-supplied `le` attribute on a native histogram must not duplicate
// the bucket `le` label — a duplicate label made the ENTIRE exposition
// unparseable (one poisoned series took down the whole /metrics page).
func TestSerialize_HistogramWithReservedLeAttribute(t *testing.T) {
	sum := 4.2
	records := []otelmapper.OtelRecord{{
		Name:       "http.server.duration",
		Unit:       "s",
		Type:       "histogram",
		Attributes: map[string]string{"le": "sneaky", "route": "/x"},
		Value:      6,
		Histogram: &datapoint.HistogramValue{
			Count: 6, Sum: &sum,
			BucketCounts: []uint64{1, 2, 3}, ExplicitBounds: []float64{0.1, 0.5},
		},
	}}
	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	body := buf.String()

	p := newTextParser()
	if _, err := p.TextToMetricFamilies(strings.NewReader(body)); err != nil {
		t.Fatalf("exposition must stay parseable despite a reserved `le` attribute: %v\nbody:\n%s", err, body)
	}
	if strings.Contains(body, `le="sneaky"`) {
		t.Errorf("sender-supplied le must be stripped from the histogram labels, got:\n%s", body)
	}
	// The genuine bucket ladder must still be present and well-formed.
	for _, want := range []string{`le="0.1"`, `le="0.5"`, `le="+Inf"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing bucket %s in:\n%s", want, body)
		}
	}
}

func keys(m map[string]*dto.MetricFamily) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSerialize_DetectsLabelCollision exercises the M2 collision path —
// two distinct OTel attribute names that collapse to the same Prometheus
// label after dot→underscore translation must produce a warn-once log.
func TestSerialize_DetectsLabelCollision(t *testing.T) {
	resetSerializerWarnDedupForTest()
	var warnings []string
	SetSerializerWarnFunc(func(format string, args ...interface{}) {
		warnings = append(warnings, format)
	})
	t.Cleanup(func() { SetSerializerWarnFunc(nil) })

	records := []otelmapper.OtelRecord{
		{
			Name: "x.metric",
			Unit: "1",
			Type: "gauge",
			Attributes: map[string]string{
				"cpu.mode": "user",
				"cpu_mode": "kernel", // collides after dot→underscore
			},
			Value: 1,
		},
	}
	var buf bytes.Buffer
	_ = SerializeToTextExposition(records, &buf, SerializeOptions{})

	if len(warnings) == 0 {
		t.Errorf("expected at least 1 label-collision warning, got 0")
	}
}

// TestSerialize_LabelCollisionDedup verifies that the same collision in
// successive scrapes warns only once across the process lifetime.
func TestSerialize_LabelCollisionDedup(t *testing.T) {
	resetSerializerWarnDedupForTest()
	var count int
	SetSerializerWarnFunc(func(format string, args ...interface{}) { count++ })
	t.Cleanup(func() { SetSerializerWarnFunc(nil) })

	records := []otelmapper.OtelRecord{
		{
			Name:       "z.metric",
			Unit:       "1",
			Type:       "gauge",
			Attributes: map[string]string{"a.b": "1", "a_b": "2"},
			Value:      1,
		},
	}
	for i := 0; i < 5; i++ {
		var buf bytes.Buffer
		_ = SerializeToTextExposition(records, &buf, SerializeOptions{})
	}
	if count != 1 {
		t.Errorf("warn-once dedup broken: %d warnings across 5 scrapes, want 1", count)
	}
}

// TestSerialize_TypeConflictWarnerDirect exercises the warnTypeConflict
// helper directly — it's defensive against future schema drift but cannot
// fire from the current OTel→Prom name function (different OTel types
// produce different Prom names because counters get _total appended).
// We test the warner itself so the dedup contract stays correct.
func TestSerialize_TypeConflictWarnerDirect(t *testing.T) {
	resetSerializerWarnDedupForTest()
	var count int
	SetSerializerWarnFunc(func(format string, args ...interface{}) { count++ })
	t.Cleanup(func() { SetSerializerWarnFunc(nil) })

	for i := 0; i < 3; i++ {
		warnTypeConflict("senhub_x_y", "x.y", "gauge", "x.y", "counter")
	}
	if count != 1 {
		t.Errorf("warnTypeConflict should be deduplicated; got %d warnings", count)
	}

	// Different promName → fresh warn key → another warning fires.
	warnTypeConflict("senhub_z", "z", "gauge", "z", "counter")
	if count != 2 {
		t.Errorf("warnTypeConflict should fire for distinct promNames; got %d", count)
	}
}

// TestSerialize_FiltersInternalUnitAttribute confirms the "unit" attribute
// is stripped from the emitted Prometheus labels. The unit is already
// encoded in the metric-name suffix (_bytes, _seconds, etc.), so emitting
// it again as a label key is redundant and confusing.
func TestSerialize_FiltersInternalUnitAttribute(t *testing.T) {
	records := []otelmapper.OtelRecord{
		{
			Name: "system.memory.usage",
			Unit: "By",
			Type: "updowncounter",
			Attributes: map[string]string{
				"system.memory.state": "used",
				"unit":                "Bytes",
				"host":                "host-1",
			},
			Value: 12345,
		},
	}
	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, `unit="Bytes"`) || strings.Contains(body, `unit=`) {
		t.Errorf("emitted output should not contain unit= label, got:\n%s", body)
	}
	// Sanity: the other attributes should still appear.
	if !strings.Contains(body, `system_memory_state="used"`) {
		t.Errorf("legitimate attribute lost; body:\n%s", body)
	}
}

// TestSerialize_UnitConflictWarner: two records sharing the same Prom name
// (same OTel name + same OTel type implying same suffix path) but DIFFERENT
// raw OTel units. In practice this is hard to reach via the YAMLs in the
// repo, but exercises the warnUnitConflict helper.
func TestSerialize_UnitConflictWarner(t *testing.T) {
	resetSerializerWarnDedupForTest()
	var count int
	SetSerializerWarnFunc(func(format string, args ...interface{}) { count++ })
	t.Cleanup(func() { SetSerializerWarnFunc(nil) })

	for i := 0; i < 3; i++ {
		warnUnitConflict("senhub_x_y", "x.y", "1", "x.y", "By")
	}
	if count != 1 {
		t.Errorf("warnUnitConflict should be deduplicated; got %d warnings", count)
	}
}

func TestSerialize_HistogramNative(t *testing.T) {
	sum, minV, maxV := 4.2, 0.05, 0.9
	records := []otelmapper.OtelRecord{{
		Name:        "http.server.duration",
		Unit:        "s",
		Type:        "histogram",
		Attributes:  map[string]string{"probe_name": "edge_in"},
		Value:       6,
		Description: "HTTP request duration",
		Histogram: &datapoint.HistogramValue{
			Count:          6,
			Sum:            &sum,
			Min:            &minV,
			Max:            &maxV,
			BucketCounts:   []uint64{1, 2, 3},
			ExplicitBounds: []float64{0.1, 0.5},
		},
	}}

	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	t.Logf("Output:\n%s", body)

	wantLines := []string{
		`# TYPE senhub_http_server_duration_seconds histogram`,
		`senhub_http_server_duration_seconds_bucket{le="0.1",probe_name="edge_in"} 1`,
		`senhub_http_server_duration_seconds_bucket{le="0.5",probe_name="edge_in"} 3`,
		`senhub_http_server_duration_seconds_bucket{le="+Inf",probe_name="edge_in"} 6`,
		`senhub_http_server_duration_seconds_sum{probe_name="edge_in"} 4.2`,
		`senhub_http_server_duration_seconds_count{probe_name="edge_in"} 6`,
	}
	for _, line := range wantLines {
		if !strings.Contains(body, line) {
			t.Errorf("missing line %q in body:\n%s", line, body)
		}
	}

	// Round-trip: the canonical Prometheus parser must see one histogram
	// family with cumulative buckets.
	p := newTextParser()
	parsed, err := p.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("expfmt parse failed: %v\nbody:\n%s", err, body)
	}
	fam, ok := parsed["senhub_http_server_duration_seconds"]
	if !ok {
		t.Fatalf("histogram family missing; got keys: %v", keys(parsed))
	}
	if fam.GetType() != dto.MetricType_HISTOGRAM {
		t.Errorf("family type=%v, want HISTOGRAM", fam.GetType())
	}
	h := fam.GetMetric()[0].GetHistogram()
	if h.GetSampleCount() != 6 {
		t.Errorf("sample count=%d, want 6", h.GetSampleCount())
	}
	if h.GetSampleSum() != 4.2 {
		t.Errorf("sample sum=%v, want 4.2", h.GetSampleSum())
	}
}

func TestSerialize_HistogramWithoutSumOmitsSumLine(t *testing.T) {
	records := []otelmapper.OtelRecord{{
		Name:       "h",
		Unit:       "1",
		Type:       "histogram",
		Attributes: map[string]string{},
		Value:      2,
		Histogram:  &datapoint.HistogramValue{Count: 2, BucketCounts: []uint64{2}},
	}}
	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, "_sum") {
		t.Errorf("optional Sum absent — no _sum line expected:\n%s", body)
	}
	if !strings.Contains(body, `senhub_h_ratio_bucket{le="+Inf"} 2`) {
		t.Errorf("missing terminal +Inf bucket:\n%s", body)
	}
	if !strings.Contains(body, "senhub_h_ratio_count 2") {
		t.Errorf("missing _count line:\n%s", body)
	}
}

func TestSerialize_HistogramTypeWithoutPayloadFallsBackToScalar(t *testing.T) {
	// Defensive: a "histogram"-typed record without payload must serialize
	// as a plain gauge sample on its scalar value — never panic, never emit
	// a broken histogram family.
	records := []otelmapper.OtelRecord{{
		Name:       "h",
		Unit:       "1",
		Type:       "histogram",
		Attributes: map[string]string{"probe_name": "edge_in"},
		Value:      6,
	}}
	var buf bytes.Buffer
	if err := SerializeToTextExposition(records, &buf, SerializeOptions{}); err != nil {
		t.Fatalf("Serialize err: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, "# TYPE senhub_h_ratio gauge") {
		t.Errorf("payload-less histogram should degrade to gauge:\n%s", body)
	}
	if !strings.Contains(body, `senhub_h_ratio{probe_name="edge_in"} 6`) {
		t.Errorf("scalar fallback sample missing:\n%s", body)
	}
	p := newTextParser()
	if _, err := p.TextToMetricFamilies(strings.NewReader(body)); err != nil {
		t.Fatalf("expfmt parse failed: %v\nbody:\n%s", err, body)
	}
}
