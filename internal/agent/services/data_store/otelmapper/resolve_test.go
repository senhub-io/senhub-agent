package otelmapper

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func TestResolve_SimpleGauge(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
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
	m := CacheMetric{
		ProbeName:  "cpu-primary",
		ProbeType:  "cpu",
		MetricName: "cpu_usage_total",
		Value:      42.5,
		Unit:       "%",
		Tags:       map[string]string{"probe_type": "cpu", "probe_name": "cpu-primary"},
	}
	recs, err := Resolve(def, m, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.Name != "system.cpu.utilization" {
		t.Errorf("name=%q", r.Name)
	}
	if !floatApprox(r.Value, 0.425) {
		t.Errorf("value=%v (expected 0.425 after %% → ratio)", r.Value)
	}
	if r.Attributes["probe_name"] != "cpu-primary" {
		t.Errorf("probe_name label missing or wrong: %v", r.Attributes)
	}
	if r.Attributes["probe_type"] != "cpu" {
		t.Errorf("probe_type label missing or wrong: %v", r.Attributes)
	}
}

func TestResolve_TagToAttribute(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "cpu_core_usage",
				Unit: "%",
				Otel: &transformers.OtelMapping{
					Name: "system.cpu.utilization",
					Unit: "1",
					Type: "gauge",
				},
				TagToAttribute: map[string]string{
					"core": "cpu.logical_number",
				},
			},
		},
	}
	m := CacheMetric{
		ProbeName:  "cpu",
		ProbeType:  "cpu",
		MetricName: "cpu_core_usage",
		Value:      10,
		Unit:       "%",
		Tags:       map[string]string{"core": "2", "probe_type": "cpu"},
	}
	recs, _ := Resolve(def, m, DefaultResolveOptions())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Attributes["cpu.logical_number"] != "2" {
		t.Errorf("cpu.logical_number not mapped: %v", recs[0].Attributes)
	}
	if _, leaked := recs[0].Attributes["core"]; leaked {
		t.Errorf("raw tag 'core' leaked as attribute: %v", recs[0].Attributes)
	}
}

func TestResolve_StaticAttributes(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName: "cpu",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "cpu_user",
				Unit: "s",
				Otel: &transformers.OtelMapping{
					Name:       "system.cpu.time",
					Unit:       "s",
					Type:       "counter",
					Attributes: map[string]string{"cpu.mode": "user"},
				},
			},
		},
	}
	m := CacheMetric{
		ProbeName:  "cpu",
		ProbeType:  "cpu",
		MetricName: "cpu_user",
		Value:      123.4,
		Unit:       "s",
	}
	recs, _ := Resolve(def, m, DefaultResolveOptions())
	if recs[0].Attributes["cpu.mode"] != "user" {
		t.Errorf("static attribute cpu.mode=user missing: %v", recs[0].Attributes)
	}
}

func TestResolve_Skip(t *testing.T) {
	def := &transformers.ProbeDefinition{
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
	m := CacheMetric{ProbeType: "syslog", MetricName: "syslog_event", Value: 1}
	recs, err := Resolve(def, m, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("Resolve err on skipped metric: %v", err)
	}
	if recs != nil {
		t.Errorf("expected nil records for skipped, got %v", recs)
	}
}

func TestResolve_Expand(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName: "redfish",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "hardware.storage.drive.health",
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
	// Cache value 1 ("Warning" in sfs.redfish.health) → hw.state=degraded should be 1, others 0.
	m := CacheMetric{
		ProbeName:  "redfish-lab",
		ProbeType:  "redfish",
		MetricName: "hardware.storage.drive.health",
		Value:      1,
		Unit:       "#",
		Tags:       map[string]string{"drive_id": "disk.bay.0"},
	}
	recs, err := Resolve(def, m, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if len(recs) != 4 {
		t.Fatalf("expected 4 expanded records, got %d", len(recs))
	}

	var gotStates []string
	matchingCount := 0
	for _, r := range recs {
		if r.Attributes["hw.type"] != "physical_disk" {
			t.Errorf("hw.type not propagated: %v", r.Attributes)
		}
		if r.Attributes["hw.id"] != "disk.bay.0" {
			t.Errorf("hw.id not mapped from drive_id: %v", r.Attributes)
		}
		state := r.Attributes["hw.state"]
		gotStates = append(gotStates, state)
		if state == "degraded" {
			if r.Value != 1 {
				t.Errorf("degraded should be 1, got %v", r.Value)
			}
			matchingCount++
		} else {
			if r.Value != 0 {
				t.Errorf("state=%q should be 0, got %v", state, r.Value)
			}
		}
	}
	if matchingCount != 1 {
		t.Errorf("expected exactly one matching state, got %d (states: %v)", matchingCount, gotStates)
	}
}

func TestResolve_MissingOtel(t *testing.T) {
	def := &transformers.ProbeDefinition{
		ProbeName: "x",
		Metrics: []transformers.MetricDefinition{
			{Name: "foo"}, // no Otel field
		},
	}
	m := CacheMetric{ProbeType: "x", MetricName: "foo", Value: 1}
	_, err := Resolve(def, m, DefaultResolveOptions())
	if err == nil {
		t.Fatal("expected error for missing otel mapping")
	}
}

func TestResolve_UnknownMetric(t *testing.T) {
	def := &transformers.ProbeDefinition{ProbeName: "x"}
	m := CacheMetric{ProbeType: "x", MetricName: "nope"}
	_, err := Resolve(def, m, DefaultResolveOptions())
	if err == nil {
		t.Fatal("expected error for unknown metric")
	}
}
