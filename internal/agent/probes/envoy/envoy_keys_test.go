package envoy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func findDataPointByName(points []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, dp := range points {
		if dp.Name == name {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

func tagValueFor(dp data_store.DataPoint, key string) (string, bool) {
	for _, tag := range dp.Tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func TestCollect_IdentityKeysAndCheckResult_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" || r.URL.Query().Get("format") != "prometheus" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(minimalEnvoyStats)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	const instanceID = "envoy-1"
	p := newTestProbe(t, map[string]interface{}{
		"endpoint":      srv.URL,
		"instance_name": instanceID,
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		id, ok := tagValueFor(dp, "service.instance.id")
		if !ok {
			t.Errorf("%s: missing service.instance.id tag", dp.Name)
			continue
		}
		if id != instanceID {
			t.Errorf("%s: service.instance.id = %q, want %q", dp.Name, id, instanceID)
		}
	}

	check, ok := findDataPointByName(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("%s datapoint missing", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("%s = %v, want 0", dbcommon.CheckResultMetric, check.Value)
	}
	metricType, ok := tagValueFor(check, "metric_type")
	if !ok || metricType != "status" {
		t.Errorf("%s metric_type = %q, want %q", dbcommon.CheckResultMetric, metricType, "status")
	}
}

func TestCollect_IdentityKeysAndCheckResult_FailedConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"endpoint": srv.URL,
	})
	p.entitySrc.hostID = func() string { return "host-1" }

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up, ok := findDataPointByName(points, "senhub.envoy.up")
	if !ok {
		t.Fatal("senhub.envoy.up datapoint missing")
	}
	if up.Value != 0 {
		t.Errorf("senhub.envoy.up = %v, want 0", up.Value)
	}
	id, ok := tagValueFor(up, "service.instance.id")
	if ok {
		t.Errorf("senhub.envoy.up service.instance.id = %q before any entity, want none", id)
	}

	check, ok := findDataPointByName(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("%s datapoint missing", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckTargetDown))
	}
	metricType, ok := tagValueFor(check, "metric_type")
	if !ok || metricType != "status" {
		t.Errorf("%s metric_type = %q, want %q", dbcommon.CheckResultMetric, metricType, "status")
	}
	if id, ok := tagValueFor(check, "service.instance.id"); ok {
		t.Errorf("%s service.instance.id = %q before any entity, want none", dbcommon.CheckResultMetric, id)
	}
}
