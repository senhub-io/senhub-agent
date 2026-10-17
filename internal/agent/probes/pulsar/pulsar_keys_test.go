package pulsar

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
)

func TestCollect_SuccessIdentityAndCheckResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v2/brokers/ready":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(brokerMetrics))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"endpoint":      srv.URL,
		"instance_name": "pulsar:test",
	})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	for _, dp := range points {
		if got := tagValue(dp, "service.instance.id"); got != "pulsar:test" {
			t.Errorf("%s service.instance.id = %q, want %q", dp.Name, got, "pulsar:test")
		}
	}

	check := indexByName(points)[dbcommon.CheckResultMetric]
	if len(check) != 1 {
		t.Fatalf("%s points = %d, want 1", dbcommon.CheckResultMetric, len(check))
	}
	if check[0].Value != float64(dbcommon.CheckOK) {
		t.Errorf("%s value = %v, want %v", dbcommon.CheckResultMetric, check[0].Value, float64(dbcommon.CheckOK))
	}
	if mt := tagValue(check[0], "metric_type"); mt != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, mt)
	}
}

func TestCollect_FailedConnectionIdentityAndCheckResult(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"endpoint": "http://127.0.0.1:1",
		"timeout":  1,
	})
	p.entitySrc.hostID = func() string { return "pulsar@testhost" }

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not error on unreachable broker: %v", err)
	}
	got := indexByName(points)

	up := got["senhub.pulsar.up"]
	if len(up) != 1 || up[0].Value != 0 {
		t.Errorf("senhub.pulsar.up = %+v, want one point with value 0", up)
	}
	if gotID := tagValue(up[0], "service.instance.id"); gotID != "pulsar@testhost" {
		t.Errorf("up service.instance.id = %q, want %q", gotID, "pulsar@testhost")
	}

	check := got[dbcommon.CheckResultMetric]
	if len(check) != 1 {
		t.Fatalf("%s points = %d, want 1", dbcommon.CheckResultMetric, len(check))
	}
	if check[0].Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%s value = %v, want %v", dbcommon.CheckResultMetric, check[0].Value, float64(dbcommon.CheckTargetDown))
	}
	if mt := tagValue(check[0], "metric_type"); mt != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, mt)
	}
	if gotID := tagValue(check[0], "service.instance.id"); gotID != "pulsar@testhost" {
		t.Errorf("%s service.instance.id = %q, want %q", dbcommon.CheckResultMetric, gotID, "pulsar@testhost")
	}
}
