package tomcat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func hasTagValue(tags []tags.Tag, key, want string) bool {
	for _, tg := range tags {
		if tg.Key == key && tg.Value == want {
			return true
		}
	}
	return false
}

func TestCollect_EntityKeysAndCheckResult_Success(t *testing.T) {
	srv := newTestServer(map[string]interface{}{
		"java.lang:type=Threading/ThreadCount": float64(42),
	})
	defer srv.Close()

	probe, err := NewTomcatProbe(map[string]interface{}{
		"jolokia_url":   srv.URL + "/jolokia",
		"instance_name": "tomcat-success",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewTomcatProbe: %v", err)
	}
	p := probe.(*TomcatProbe)
	p.SetName("tomcat-success")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no data points")
	}

	checkFound := false
	for _, dp := range points {
		if !hasTagValue(dp.Tags, "service.instance.id", "tomcat-success") {
			t.Errorf("datapoint %s missing service.instance.id=tomcat-success, tags=%v", dp.Name, dp.Tags)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			checkFound = true
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("check result = %v, want %v", dp.Value, float64(dbcommon.CheckOK))
			}
			if !hasTagValue(dp.Tags, "metric_type", "status") {
				t.Errorf("check result missing metric_type=status, tags=%v", dp.Tags)
			}
		}
	}
	if !checkFound {
		t.Fatalf("check result metric %q not found", dbcommon.CheckResultMetric)
	}
}

func TestCollect_EntityKeysAndCheckResult_ConnectionDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	probe, err := NewTomcatProbe(map[string]interface{}{
		"jolokia_url":   srv.URL + "/jolokia",
		"instance_name": "tomcat-down",
	}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewTomcatProbe: %v", err)
	}
	p := probe.(*TomcatProbe)
	p.SetName("tomcat-down")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no data points")
	}

	upFound := false
	checkFound := false
	var checkVal float64
	for _, dp := range points {
		if !hasTagValue(dp.Tags, "service.instance.id", "tomcat-down") {
			t.Errorf("datapoint %s missing service.instance.id=tomcat-down, tags=%v", dp.Name, dp.Tags)
		}
		if dp.Name == "senhub.tomcat.up" {
			upFound = true
			if dp.Value != 0 {
				t.Errorf("senhub.tomcat.up = %v, want 0", dp.Value)
			}
			if !hasTagValue(dp.Tags, "metric_type", "availability") {
				t.Errorf("senhub.tomcat.up missing metric_type=availability, tags=%v", dp.Tags)
			}
		}
		if dp.Name == dbcommon.CheckResultMetric {
			checkFound = true
			checkVal = dp.Value
			if !hasTagValue(dp.Tags, "metric_type", "status") {
				t.Errorf("check result missing metric_type=status, tags=%v", dp.Tags)
			}
		}
	}
	if !upFound {
		t.Fatal("senhub.tomcat.up not found")
	}
	if !checkFound {
		t.Fatalf("check result metric %q not found", dbcommon.CheckResultMetric)
	}
	if checkVal != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result = %v, want %v", checkVal, float64(dbcommon.CheckTargetDown))
	}
}
