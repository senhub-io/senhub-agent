package nginx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func tagValue(ts []tags.Tag, key string) (string, bool) {
	for _, tag := range ts {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func metricValue(points []data_store.DataPoint, name string) (float64, bool) {
	for _, p := range points {
		if p.Name == name {
			return p.Value, true
		}
	}
	return 0, false
}

func assertNginxIdentityAndCheckResult(t *testing.T, points []data_store.DataPoint, wantID string, wantCheck float64) {
	t.Helper()
	if len(points) == 0 {
		t.Fatal("expected datapoints, got none")
	}
	for _, p := range points {
		gotID, ok := tagValue(p.Tags, "service.instance.id")
		if !ok {
			t.Errorf("datapoint %s missing service.instance.id tag", p.Name)
			continue
		}
		if gotID != wantID {
			t.Errorf("datapoint %s service.instance.id = %q, want %q", p.Name, gotID, wantID)
		}
	}
	gotCheck, ok := metricValue(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("missing %s datapoint", dbcommon.CheckResultMetric)
	}
	if gotCheck != wantCheck {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, gotCheck, wantCheck)
	}
}

func TestCollect_IdentityTagsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleStubStatus))
	}))
	defer srv.Close()

	probe, err := NewNginxProbe(map[string]interface{}{"endpoint": srv.URL, "instance_name": "web01"}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertNginxIdentityAndCheckResult(t, points, "web01", float64(dbcommon.CheckOK))
}

func TestCollect_IdentityTagsDown(t *testing.T) {
	probe, err := NewNginxProbe(map[string]interface{}{
		"endpoint":      "http://127.0.0.1:1", // port 1 is never open
		"timeout":       1,
		"instance_name": "web01",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewNginxProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error for a down target, got: %v", err)
	}
	assertNginxIdentityAndCheckResult(t, points, "web01", float64(dbcommon.CheckTargetDown))

	up, ok := metricValue(points, "senhub.nginx.up")
	if !ok {
		t.Fatal("missing senhub.nginx.up datapoint")
	}
	if up != 0 {
		t.Errorf("senhub.nginx.up = %v, want 0 when endpoint unreachable", up)
	}
}
