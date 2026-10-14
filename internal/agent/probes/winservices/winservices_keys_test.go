package winservices

import (
	"context"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func tagValue(pt data_store.DataPoint, key string) string {
	for _, tag := range pt.Tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func TestCollectSuccessCarriesIdentityAndCheckResultOK(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.getHostID = func() string { return "test-host-uuid" }
	p.collect = func(_ []string) ([]serviceState, error) {
		return []serviceState{{name: "Spooler", state: stateRunning, startType: startAutomatic}}, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	wantID := "winservices@test-host-uuid"
	// The probe's own series name the probe entity; a Windows service's series
	// describe that service, which is not this entity.
	for _, pt := range points {
		want := wantID
		if strings.HasPrefix(pt.Name, "windows.service.") {
			want = ""
		}
		if got := tagValue(pt, idKeyServiceInstanceID); got != want {
			t.Errorf("datapoint %q identity tag = %q, want %q", pt.Name, got, want)
		}
	}

	check, ok := findPoint(points, dbcommon.CheckResultMetric, "")
	if !ok {
		t.Fatalf("missing check result datapoint %s", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckOK) {
		t.Errorf("check result = %v, want %d", check.Value, dbcommon.CheckOK)
	}
}

func TestCollectFailureCarriesFallbackIdentityAndClassifiedResult(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	p.getHostID = func() string { return "" }
	p.collect = func(_ []string) ([]serviceState, error) {
		return nil, context.DeadlineExceeded
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return an error on SCM failure: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	up, ok := findPoint(points, "senhub.winservices.up", "")
	if !ok {
		t.Fatal("missing senhub.winservices.up")
	}
	if up.Value != 0 {
		t.Errorf("up = %v, want 0 on collection failure", up.Value)
	}

	for _, pt := range points {
		if got := tagValue(pt, idKeyServiceInstanceID); got != "" {
			t.Errorf("datapoint %q identity tag = %q without a host id, want none", pt.Name, got)
		}
	}

	check, ok := findPoint(points, dbcommon.CheckResultMetric, "")
	if !ok {
		t.Fatalf("missing check result datapoint %s", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckTimeout) {
		t.Errorf("check result = %v, want %d", check.Value, dbcommon.CheckTimeout)
	}
}
