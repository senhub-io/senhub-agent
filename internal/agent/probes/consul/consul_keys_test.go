package consul

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func findPoint(points []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, dp := range points {
		if dp.Name == name {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

func TestConsulProbe_AllDatapointsCarryInstanceIDAndCheckResultOK(t *testing.T) {
	srv := newTestServer(t, promFixture, agentSelfFixture, 1, 0, 10)
	defer srv.Close()

	p, err := NewConsulProbe(map[string]interface{}{
		"endpoint":      srv.URL,
		"instance_name": "consul-test-id",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewConsulProbe: %v", err)
	}
	cp := p.(*ConsulProbe)
	wantID := cp.entitySrc.InstanceID()
	if wantID != "consul-test-id" {
		t.Fatalf("InstanceID: got %q, want %q", wantID, "consul-test-id")
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no points")
	}

	for _, dp := range points {
		got, ok := tagValue(dp.Tags, "service.instance.id")
		if !ok {
			t.Errorf("point %q missing service.instance.id", dp.Name)
			continue
		}
		if got != wantID {
			t.Errorf("point %q service.instance.id: got %q, want %q", dp.Name, got, wantID)
		}
	}

	check, ok := findPoint(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("missing %q", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("%q value: got %v, want 0", dbcommon.CheckResultMetric, check.Value)
	}
	mt, ok := tagValue(check.Tags, "metric_type")
	if !ok || mt != "status" {
		t.Errorf("%q metric_type: got %q (present=%v), want status", dbcommon.CheckResultMetric, mt, ok)
	}
}

func TestConsulProbe_CheckResultClassifiesConnectionFailure(t *testing.T) {
	p, err := NewConsulProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewConsulProbe: %v", err)
	}
	cp := p.(*ConsulProbe)
	wantID := cp.entitySrc.InstanceID()
	if wantID != "" {
		t.Fatalf("id %q before any entity was emitted, want none", wantID)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must return nil error on unreachable agent, got: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no points")
	}

	if points[0].Name != "senhub.consul.up" {
		t.Errorf("first point: got %q, want senhub.consul.up", points[0].Name)
	}
	if points[0].Value != 0 {
		t.Errorf("senhub.consul.up on failure: got %v, want 0", points[0].Value)
	}

	for _, dp := range points {
		got, ok := tagValue(dp.Tags, "service.instance.id")
		if !ok {
			t.Errorf("point %q missing service.instance.id", dp.Name)
			continue
		}
		if got != wantID {
			t.Errorf("point %q service.instance.id: got %q, want %q", dp.Name, got, wantID)
		}
	}

	check, ok := findPoint(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("missing %q", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%q value: got %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckTargetDown))
	}
	mt, ok := tagValue(check.Tags, "metric_type")
	if !ok || mt != "status" {
		t.Errorf("%q metric_type: got %q (present=%v), want status", dbcommon.CheckResultMetric, mt, ok)
	}
}
