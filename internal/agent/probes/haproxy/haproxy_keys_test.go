package haproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func TestCollect_AllDatapointsCarryServiceInstanceIDAndCheckResultOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	probe := makeTestProbe(srv.URL)
	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no points")
	}

	const wantID = "haproxy@test-host-id"
	for _, point := range points {
		if got := tagValue(point.Tags, "service.instance.id"); got != wantID {
			t.Errorf("metric %q service.instance.id = %q, want %q", point.Name, got, wantID)
		}
	}

	checkResult := findMetricValue(points, dbcommon.CheckResultMetric)
	if checkResult == nil {
		t.Fatal("senhub.probe.check.result not emitted")
	}
	if *checkResult != 0 {
		t.Errorf("senhub.probe.check.result = %v, want 0", *checkResult)
	}

	for _, point := range points {
		if point.Name != dbcommon.CheckResultMetric {
			continue
		}
		if got := tagValue(point.Tags, "metric_type"); got != "status" {
			t.Errorf("check result metric_type = %q, want \"status\"", got)
		}
	}
}

func TestCollect_CheckResultPresentOnFailedConnection(t *testing.T) {
	probe := makeTestProbe("http://127.0.0.1:19999/stats;csv")
	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() should not propagate errors, got: %v", err)
	}

	upMetric := findMetricValue(points, "senhub.haproxy.up")
	if upMetric == nil {
		t.Fatal("senhub.haproxy.up not emitted on failure")
	}
	if *upMetric != 0 {
		t.Errorf("senhub.haproxy.up = %v, want 0 on unreachable target", *upMetric)
	}

	const wantID = "haproxy@test-host-id"
	for _, point := range points {
		if got := tagValue(point.Tags, "service.instance.id"); got != wantID {
			t.Errorf("metric %q service.instance.id = %q, want %q", point.Name, got, wantID)
		}
	}

	checkResult := findMetricValue(points, dbcommon.CheckResultMetric)
	if checkResult == nil {
		t.Fatal("senhub.probe.check.result not emitted on failure")
	}
	if *checkResult != float64(dbcommon.CheckTargetDown) {
		t.Errorf("senhub.probe.check.result = %v, want %v (target down)", *checkResult, float64(dbcommon.CheckTargetDown))
	}
}

func tagValue(tgs []tags.Tag, key string) string {
	for _, tag := range tgs {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}
