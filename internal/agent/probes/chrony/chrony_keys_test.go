package chrony

import (
	"fmt"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
)

func TestCollect_Success_CarriesEntityIDAndCheckResult(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	p.SetName("chrony-test")
	p.run = func() trackingResult {
		return trackingResult{
			stratum:         2,
			systemTimeS:     0.000123,
			freqPPM:         -3.5,
			skewPPM:         0.05,
			rootDelayS:      0.0015,
			rootDispersionS: 0.0025,
			leapStatus:      leapNormal,
		}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	expectedID := p.entitySrc.ServiceInstanceID()
	if expectedID == "" {
		t.Fatal("entity source service.instance.id is empty")
	}
	for _, pt := range points {
		if !hasTag(pt.Tags, "service.instance.id", expectedID) {
			t.Errorf("point %s missing service.instance.id=%s", pt.Name, expectedID)
		}
	}

	check := findByName(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatal("check result point missing")
	}
	if check.Value != 0 {
		t.Errorf("check result = %v, want 0", check.Value)
	}
	if !hasTag(check.Tags, "metric_type", "status") {
		t.Error("check result missing metric_type=status")
	}
}

func TestCollect_Failure_CarriesEntityIDAndCheckResult(t *testing.T) {
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	p.SetName("chrony-test")

	runErr := fmt.Errorf("chronyc: exec: \"chronyc\": executable file not found in $PATH")
	p.run = func() trackingResult {
		return trackingResult{err: runErr}
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up := findByName(points, "senhub.chrony.up")
	if up == nil {
		t.Fatal("senhub.chrony.up not found")
	}
	if up.Value != 0 {
		t.Errorf("senhub.chrony.up = %v, want 0", up.Value)
	}
	if !hasTag(up.Tags, "metric_type", "time_sync") {
		t.Error("senhub.chrony.up missing metric_type=time_sync")
	}

	expectedID := p.entitySrc.ServiceInstanceID()
	if expectedID == "" {
		t.Fatal("entity source service.instance.id is empty")
	}
	if !hasTag(up.Tags, "service.instance.id", expectedID) {
		t.Errorf("senhub.chrony.up missing service.instance.id=%s", expectedID)
	}

	check := findByName(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatal("check result point missing")
	}
	want := float64(dbcommon.ClassifyError(runErr))
	if check.Value != want {
		t.Errorf("check result = %v, want %v", check.Value, want)
	}
	if !hasTag(check.Tags, "metric_type", "status") {
		t.Error("check result missing metric_type=status")
	}
	if !hasTag(check.Tags, "service.instance.id", expectedID) {
		t.Errorf("check result missing service.instance.id=%s", expectedID)
	}
}

func TestEntitySourceServiceInstanceIDFallback(t *testing.T) {
	s := newChronyEntitySource()
	if got, want := s.ServiceInstanceID(), ""; got != want {
		t.Errorf("fallback ServiceInstanceID = %q, want %q", got, want)
	}
}
