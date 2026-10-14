package apache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func hasTag(tags []tags.Tag, key, value string) bool {
	for _, tg := range tags {
		if tg.Key == key && tg.Value == value {
			return true
		}
	}
	return false
}

func TestCollect_EveryDatapointCarriesServiceInstanceIDAndCheckResultOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleModStatus)
	}))
	defer srv.Close()

	p, err := NewApacheProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	wantID := ap.entitySrc.InstanceID()
	if wantID == "" {
		t.Fatal("entity source instance id is empty")
	}

	var sawCheck bool
	for _, dp := range points {
		if !hasTag(dp.Tags, "service.instance.id", wantID) {
			t.Errorf("datapoint %s tags %v: missing service.instance.id=%s", dp.Name, dp.Tags, wantID)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			sawCheck = true
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("check result value = %v, want 0", dp.Value)
			}
			if !hasTag(dp.Tags, "metric_type", "status") {
				t.Errorf("check result missing metric_type=status: %v", dp.Tags)
			}
		}
	}
	if !sawCheck {
		t.Fatalf("no %s datapoint", dbcommon.CheckResultMetric)
	}
}

func TestCollect_ConnectionFailureCheckResultAndIdentityTag(t *testing.T) {
	p, err := NewApacheProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999/server-status?auto",
		"timeout":  1,
	}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewApacheProbe: %v", err)
	}
	ap := p.(*apacheProbe)
	ap.SetName("apache-test")

	points, err := ap.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error on connection failure: %v", err)
	}

	wantID := ap.entitySrc.InstanceID()
	if wantID == "" {
		t.Fatal("entity source instance id is empty")
	}

	var sawUp, sawCheck bool
	for _, dp := range points {
		if !hasTag(dp.Tags, "service.instance.id", wantID) {
			t.Errorf("datapoint %s tags %v: missing service.instance.id=%s", dp.Name, dp.Tags, wantID)
		}
		switch dp.Name {
		case "senhub.apache.up":
			sawUp = true
			if dp.Value != 0 {
				t.Errorf("senhub.apache.up = %v, want 0", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			sawCheck = true
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("check result value = %v, want %v", dp.Value, float64(dbcommon.CheckTargetDown))
			}
		}
	}
	if !sawUp {
		t.Fatal("no senhub.apache.up datapoint")
	}
	if !sawCheck {
		t.Fatalf("no %s datapoint", dbcommon.CheckResultMetric)
	}
}
