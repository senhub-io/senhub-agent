package prometheus

import (
	"bytes"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
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
	records := []OtelRecord{
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
	if _, ok := parsed["senhub_hw_status_ratio"]; !ok {
		t.Errorf("expected senhub_hw_status_ratio in parsed output; got keys: %v", keys(parsed))
	}
}

func TestSerialize_GroupsSameMetricName(t *testing.T) {
	// Two OTel records that collapse to the same Prometheus name should
	// share a single HELP/TYPE header.
	records := []OtelRecord{
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
	records := []OtelRecord{
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

func keys(m map[string]*dto.MetricFamily) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
