package wildfly

import (
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func TestCollectKeysAndCheckResult_Success(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	defer srv.Close()

	p := newTestProbe(t, srv)
	points, err := p.Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints collected")
	}

	expectedID := p.entitySrc.InstanceID()
	if expectedID == "" {
		t.Fatal("expected a non-empty service.instance.id")
	}

	checkCount := 0
	for _, dp := range points {
		if !hasTag(dp.Tags, "service.instance.id", expectedID) {
			t.Errorf("datapoint %s does not carry service.instance.id=%q: %#v", dp.Name, expectedID, dp.Tags)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			checkCount++
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("check result = %v, want %v", dp.Value, float64(dbcommon.CheckOK))
			}
			if !hasTag(dp.Tags, "metric_type", "status") {
				t.Errorf("check result metric_type tag missing: %#v", dp.Tags)
			}
		}
	}
	if checkCount != 1 {
		t.Fatalf("check result datapoint count = %d, want 1", checkCount)
	}
}

func TestCollectKeysAndCheckResult_ConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(&wildflyHandler{})
	p := newTestProbe(t, srv)
	srv.Close()

	points, err := p.Collect()
	if err != nil {
		t.Fatal(err)
	}

	expectedID := p.entitySrc.InstanceID()
	if expectedID == "" {
		t.Fatal("expected a non-empty service.instance.id")
	}
	for _, dp := range points {
		if !hasTag(dp.Tags, "service.instance.id", expectedID) {
			t.Errorf("datapoint %s does not carry service.instance.id=%q: %#v", dp.Name, expectedID, dp.Tags)
		}
	}

	up := findPoint(t, points, "senhub.wildfly.up")
	if up.Value != 0 {
		t.Fatalf("up = %v, want 0", up.Value)
	}
	if !hasTag(up.Tags, "metric_type", "availability") {
		t.Errorf("up metric_type tag missing: %#v", up.Tags)
	}

	check := findPoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Fatalf("check result = %v, want %v", check.Value, float64(dbcommon.CheckTargetDown))
	}
	if !hasTag(check.Tags, "metric_type", "status") {
		t.Errorf("check result metric_type tag missing: %#v", check.Tags)
	}
}

func hasTag(tt []tags.Tag, key, value string) bool {
	for _, tag := range tt {
		if tag.Key == key && tag.Value == value {
			return true
		}
	}
	return false
}

func findPoint(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	for _, dp := range points {
		if dp.Name == name {
			return dp
		}
	}
	t.Fatalf("no datapoint named %s", name)
	return data_store.DataPoint{}
}
