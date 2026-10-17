package winservices

import (
	"context"
	"strings"
	"testing"
	"time"

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
		if strings.HasPrefix(pt.Name, "windows.service.") || strings.HasPrefix(pt.Name, "host.service.") {
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

func TestBuildServiceDatapointsCarriesHostServiceTags(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	s := serviceState{
		name:         "Spooler",
		state:        stateRunning,
		startType:    startAutomaticDelayed,
		startDelayed: true,
		triggerStart: boolPtr(true),
	}

	pts := p.buildServiceDatapoints(s, time.Now(), "host-1", "host-name", serviceDetailFull)
	if len(pts) == 0 {
		t.Fatal("buildServiceDatapoints returned no datapoints")
	}

	for _, pt := range pts {
		if got := tagValue(pt, "host.id"); got != "host-1" {
			t.Fatalf("datapoint %q host.id = %q, want host-1", pt.Name, got)
		}
		if got := tagValue(pt, "host.service.name"); got != "Spooler" {
			t.Fatalf("datapoint %q host.service.name = %q, want Spooler", pt.Name, got)
		}
		if got := tagValue(pt, "host.name"); got != "host-name" {
			t.Fatalf("datapoint %q host.name = %q, want host-name", pt.Name, got)
		}
		if got := tagValue(pt, idKeyServiceInstanceID); got != "" {
			t.Fatalf("datapoint %q must not carry service.instance.id: %q", pt.Name, got)
		}
	}
}

func TestBuildServiceDatapointsOmitsIdentityWithoutHostID(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	s := serviceState{
		name:         "Spooler",
		state:        stateStopped,
		startType:    startManual,
		startDelayed: false,
		triggerStart: boolPtr(false),
	}

	pts := p.buildServiceDatapoints(s, time.Now(), "", "host-name", serviceDetailFull)
	if len(pts) == 0 {
		t.Fatal("buildServiceDatapoints returned no datapoints")
	}

	for _, pt := range pts {
		if got := tagValue(pt, "host.id"); got != "" {
			t.Fatalf("datapoint %q host.id = %q without hostID, want none", pt.Name, got)
		}
		if got := tagValue(pt, "host.service.name"); got != "" {
			t.Fatalf("datapoint %q host.service.name = %q without hostID, want none", pt.Name, got)
		}
		if got := tagValue(pt, "host.name"); got != "host-name" {
			t.Fatalf("datapoint %q host.name = %q, want host-name", pt.Name, got)
		}
	}
}
