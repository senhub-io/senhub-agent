package jenkins

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func setProbeEntitySource(t *testing.T, p *jenkinsProbe, instanceName, hostID string) {
	t.Helper()
	p.entitySource = newEntitySource(instanceName, "host", "8080", alwaysFailFetch, stubHostID(hostID))
	p.SetEntitySource(p.entitySource)
}

func findDataPoint(points []data_store.DataPoint, name string) *data_store.DataPoint {
	for i := range points {
		if points[i].Name == name {
			return &points[i]
		}
	}
	return nil
}

func TestCollect_AllDataPointsCarryEntityIDAndCheckResultOK(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	p := newTestProbe(t, srv.URL)
	setProbeEntitySource(t, p, "pinned-test", "h")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints")
	}

	for _, dp := range points {
		if got := tagVal(dp, idKeyServiceInstanceID); got != "pinned-test" {
			t.Errorf("%s service.instance.id = %q, want pinned-test", dp.Name, got)
		}
	}

	check := findDataPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("%s not emitted", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("%s = %v, want 0 on successful collection", dbcommon.CheckResultMetric, check.Value)
	}
	if got := tagVal(*check, "metric_type"); got != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, got)
	}
}

func TestCollect_DownCheckResultFallbackIdentity(t *testing.T) {
	srv := newTestServer(t)
	srv.Close()
	p := newTestProbe(t, srv.URL)
	setProbeEntitySource(t, p, "", "machine-fallback")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not error on a down controller: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints")
	}

	// Nothing has observed the entity yet, so it has no id to carry.
	wantID := ""
	for _, dp := range points {
		if got := tagVal(dp, idKeyServiceInstanceID); got != wantID {
			t.Errorf("%s service.instance.id = %q, want %q", dp.Name, got, wantID)
		}
	}

	up := findDataPoint(points, metricUp)
	if up == nil {
		t.Fatalf("%s not emitted", metricUp)
	}
	if up.Value != 0 {
		t.Errorf("%s = %v, want 0 when controller is down", metricUp, up.Value)
	}
	if got := tagVal(*up, "instance"); got == "" {
		t.Errorf("%s instance tag missing", metricUp)
	}
	if got := tagVal(*up, "metric_type"); got != "status" {
		t.Errorf("%s metric_type = %q, want status", metricUp, got)
	}

	check := findDataPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("%s not emitted", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%s = %v, want %d (target down)", dbcommon.CheckResultMetric, check.Value, dbcommon.CheckTargetDown)
	}
	if got := tagVal(*check, "metric_type"); got != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, got)
	}
	if got := tagVal(*check, idKeyServiceInstanceID); got != wantID {
		t.Errorf("%s service.instance.id = %q, want %q", dbcommon.CheckResultMetric, got, wantID)
	}
}
