package ceph

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func findPointByName(points []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, dp := range points {
		if dp.Name == name {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

func tagsAsMap(dp data_store.DataPoint) map[string]string {
	m := make(map[string]string, len(dp.Tags))
	for _, tag := range dp.Tags {
		m[tag.Key] = tag.Value
	}
	return m
}

func TestCollect_IdentityTagsAndCheckResultOK(t *testing.T) {
	_, probe := fakeServer(t)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	wantID := "ceph:aabbccdd-1234-5678-abcd-ef0123456789"
	for _, dp := range points {
		actual, ok := tagsAsMap(dp)[idKeyServiceInstanceID]
		if !ok {
			t.Errorf("datapoint %s missing identity tag %s", dp.Name, idKeyServiceInstanceID)
			continue
		}
		if actual != wantID {
			t.Errorf("datapoint %s tag %s = %q; want %q", dp.Name, idKeyServiceInstanceID, actual, wantID)
		}
	}

	check, ok := findPointByName(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("missing %s datapoint", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckOK) {
		t.Errorf("%s = %v; want 0", dbcommon.CheckResultMetric, check.Value)
	}
	if got := tagsAsMap(check)["metric_type"]; got != "status" {
		t.Errorf("%s metric_type = %q; want status", dbcommon.CheckResultMetric, got)
	}
	if got := tagsAsMap(check)[idKeyServiceInstanceID]; got != wantID {
		t.Errorf("%s identity tag = %q; want %q", dbcommon.CheckResultMetric, got, wantID)
	}
}

func TestCollect_AuthFailureIdentityFallbackAndCheckResult(t *testing.T) {
	probeIface, err := NewCephProbe(map[string]interface{}{
		"endpoint":   "http://127.0.0.1:1",
		"username":   "admin",
		"password":   "pass",
		"verify_tls": false,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCephProbe: %v", err)
	}
	probe := probeIface.(*CephProbe)

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect should not return an error on auth failure; got: %v", err)
	}

	fallbackID := probe.entitySrc.CurrentID()
	if fallbackID != "" {
		t.Fatalf("id %q before any entity was emitted, want none", fallbackID)
	}

	up, ok := findPointByName(points, "senhub.ceph.up")
	if !ok {
		t.Fatal("missing senhub.ceph.up datapoint")
	}
	if up.Value != 0 {
		t.Errorf("senhub.ceph.up = %v; want 0", up.Value)
	}
	upTags := tagsAsMap(up)
	if got := upTags[idKeyServiceInstanceID]; got != fallbackID {
		t.Errorf("senhub.ceph.up identity tag = %q; want fallback %q", got, fallbackID)
	}

	check, ok := findPointByName(points, dbcommon.CheckResultMetric)
	if !ok {
		t.Fatalf("missing %s datapoint", dbcommon.CheckResultMetric)
	}
	wantCode := float64(dbcommon.CheckTargetDown)
	if check.Value != wantCode {
		t.Errorf("%s = %v; want %v (target down)", dbcommon.CheckResultMetric, check.Value, wantCode)
	}
	checkTags := tagsAsMap(check)
	if got := checkTags["metric_type"]; got != "status" {
		t.Errorf("%s metric_type = %q; want status", dbcommon.CheckResultMetric, got)
	}
	if got := checkTags[idKeyServiceInstanceID]; got != fallbackID {
		t.Errorf("%s identity tag = %q; want fallback %q", dbcommon.CheckResultMetric, got, fallbackID)
	}
}
