package prometheus

import (
	"bytes"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// fakeCacheReader implements CacheReader for tests.
type fakeCacheReader struct {
	metrics []otelmapper.CacheMetric
}

func (f *fakeCacheReader) GetAll() []otelmapper.CacheMetric { return f.metrics }

// fakeDefinitionLookup implements DefinitionLookup for tests.
type fakeDefinitionLookup struct {
	defs map[string]*transformers.ProbeDefinition
}

func (f *fakeDefinitionLookup) GetProbeDefinition(probeType string) *transformers.ProbeDefinition {
	return f.defs[probeType]
}

func TestWriteExposition_EndToEnd(t *testing.T) {
	// Simulated probe definition: cpu with 3 metrics including a time counter
	// (collapsed with mode label) and a utilization gauge (% → ratio).
	cpuDef := &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
			{
				Name:        "cpu_user",
				Unit:        "s",
				Description: "Time in user mode",
				Otel: &transformers.OtelMapping{
					Name:       "system.cpu.time",
					Unit:       "s",
					Type:       "counter",
					Attributes: map[string]string{"cpu.mode": "user"},
				},
			},
			{
				Name:        "cpu_system",
				Unit:        "s",
				Description: "Time in system mode",
				Otel: &transformers.OtelMapping{
					Name:       "system.cpu.time",
					Unit:       "s",
					Type:       "counter",
					Attributes: map[string]string{"cpu.mode": "system"},
				},
			},
			{
				Name:        "cpu_usage_total",
				Unit:        "%",
				Description: "Overall CPU utilization",
				Otel: &transformers.OtelMapping{
					Name: "system.cpu.utilization",
					Unit: "1",
					Type: "gauge",
				},
			},
		},
	}

	// Redfish with a health metric that uses expand (4 states).
	redfishDef := &transformers.ProbeDefinition{
		ProbeName: "redfish",
		Metrics: []transformers.MetricDefinition{
			{
				Name:        "hardware.storage.drive.health",
				Unit:        "#",
				Description: "Drive health status",
				Otel: &transformers.OtelMapping{
					Name:       "hw.status",
					Unit:       "1",
					Type:       "updowncounter",
					Attributes: map[string]string{"hw.type": "physical_disk"},
					Expand: &transformers.ExpandDirective{
						Attribute: "hw.state",
						Mapping: map[string]int{
							"ok":       0,
							"degraded": 1,
							"failed":   2,
							"unknown":  3,
						},
					},
				},
				TagToAttribute: map[string]string{"drive_id": "hw.id"},
			},
		},
	}

	// Skipped probe (syslog).
	syslogDef := &transformers.ProbeDefinition{
		ProbeName: "syslog",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "syslog_event",
				Otel: &transformers.OtelMapping{
					Skip:   true,
					Reason: "Event conduit",
				},
			},
		},
	}

	reader := &fakeCacheReader{
		metrics: []otelmapper.CacheMetric{
			{
				ProbeName:  "cpu",
				ProbeType:  "cpu",
				MetricName: "cpu_user",
				Value:      12345.6,
				Unit:       "s",
			},
			{
				ProbeName:  "cpu",
				ProbeType:  "cpu",
				MetricName: "cpu_system",
				Value:      789.0,
				Unit:       "s",
			},
			{
				ProbeName:  "cpu",
				ProbeType:  "cpu",
				MetricName: "cpu_usage_total",
				Value:      42.5,
				Unit:       "%",
			},
			{
				ProbeName:  "redfish-prod",
				ProbeType:  "redfish",
				MetricName: "hardware.storage.drive.health",
				Value:      1, // Warning → degraded
				Unit:       "#",
				Tags:       map[string]string{"drive_id": "disk.bay.0"},
			},
			{
				ProbeName:  "syslog",
				ProbeType:  "syslog",
				MetricName: "syslog_event",
				Value:      3,
				Unit:       "#",
			},
		},
	}
	defs := &fakeDefinitionLookup{
		defs: map[string]*transformers.ProbeDefinition{
			"cpu":     cpuDef,
			"redfish": redfishDef,
			"syslog":  syslogDef,
		},
	}

	var buf bytes.Buffer
	var handlerErrors []error
	count, err := WriteExposition(reader, defs, nil, otelmapper.DefaultResolveOptions(), &buf, func(m otelmapper.CacheMetric, err error) {
		handlerErrors = append(handlerErrors, err)
	})
	if err != nil {
		t.Fatalf("WriteExposition err: %v", err)
	}
	if len(handlerErrors) > 0 {
		t.Errorf("unexpected errors reported: %v", handlerErrors)
	}

	// Expected records: 2 (cpu_time) + 1 (cpu_utilization) + 4 (expanded health) + 0 (skipped) = 7
	if count != 7 {
		t.Errorf("expected 7 records, got %d", count)
	}

	body := buf.String()
	t.Logf("Exposition:\n%s", body)

	// Round-trip through expfmt.
	p := newTextParser()
	parsed, err := p.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("expfmt parse failed: %v\nbody:\n%s", err, body)
	}

	// Verify every expected metric name is present.
	expected := []string{
		"senhub_system_cpu_time_seconds_total",
		"senhub_system_cpu_utilization_ratio",
		"senhub_hw_status",
	}
	for _, name := range expected {
		if _, ok := parsed[name]; !ok {
			t.Errorf("missing metric %q in parsed output; got %v", name, keys(parsed))
		}
	}

	// Verify the skipped probe produced NO output.
	for name := range parsed {
		if strings.Contains(name, "syslog") {
			t.Errorf("skipped metric leaked into output: %q", name)
		}
	}

	// Verify cpu_time has BOTH mode=user and mode=system labels (collapse worked).
	cpuTime := parsed["senhub_system_cpu_time_seconds_total"]
	if cpuTime == nil || len(cpuTime.GetMetric()) != 2 {
		t.Errorf("expected 2 series for senhub_system_cpu_time_seconds_total (user + system), got %v",
			cpuTime)
	}

	// Verify hw.status expanded to 4 series (one per state).
	hw := parsed["senhub_hw_status"]
	if hw == nil || len(hw.GetMetric()) != 4 {
		t.Errorf("expected 4 series for senhub_hw_status (4 states), got %v", hw)
	}

	// Verify exactly one hw.state=degraded series has value=1, others=0.
	oneCount := 0
	zeroCount := 0
	for _, m := range hw.GetMetric() {
		if m.GetGauge().GetValue() == 1 {
			oneCount++
		} else if m.GetGauge().GetValue() == 0 {
			zeroCount++
		}
	}
	if oneCount != 1 || zeroCount != 3 {
		t.Errorf("expected exactly 1 state=1 and 3 state=0, got ones=%d zeros=%d", oneCount, zeroCount)
	}
}

func TestWriteExposition_UnknownProbe(t *testing.T) {
	reader := &fakeCacheReader{
		metrics: []otelmapper.CacheMetric{
			{ProbeType: "mystery", MetricName: "x", Value: 1},
		},
	}
	defs := &fakeDefinitionLookup{defs: map[string]*transformers.ProbeDefinition{}}

	var buf bytes.Buffer
	var errs []string
	count, err := WriteExposition(reader, defs, nil, otelmapper.DefaultResolveOptions(), &buf, func(m otelmapper.CacheMetric, err error) {
		errs = append(errs, err.Error())
	})
	if err != nil {
		t.Fatalf("WriteExposition err: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 records for unknown probe, got %d", count)
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error callback, got %d", len(errs))
	}
	// Body should be empty (no TYPE/HELP lines either).
	if buf.Len() != 0 {
		t.Errorf("expected empty body, got %q", buf.String())
	}
}
