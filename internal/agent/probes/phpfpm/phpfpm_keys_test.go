package phpfpm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func hasTag(dp data_store.DataPoint, key, want string) bool {
	for _, tag := range dp.Tags {
		if tag.Key == key {
			return tag.Value == want
		}
	}
	return false
}

func TestCollectKeysCheckResultSuccess(t *testing.T) {
	payload := fpmStatus{
		Pool:               "www",
		StartSince:         3600,
		AcceptedConn:       100,
		ListenQueue:        2,
		MaxListenQueue:     10,
		IdleProcesses:      5,
		ActiveProcesses:    2,
		TotalProcesses:     7,
		MaxChildrenReached: 1,
		SlowRequests:       3,
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	probe, err := NewPHPFPMProbe(map[string]interface{}{
		"endpoint":      srv.URL + "/fpm-status",
		"instance_name": "phpfpm-test-instance",
	}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	foundCheck := false
	for _, dp := range points {
		if !hasTag(dp, "service.instance.id", "phpfpm-test-instance") {
			t.Errorf("datapoint %q missing/incorrect service.instance.id tag: tags=%v", dp.Name, dp.Tags)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			foundCheck = true
			if dp.Value != 0 {
				t.Errorf("check result = %v, want 0", dp.Value)
			}
			if !hasTag(dp, "metric_type", "status") {
				t.Errorf("check result missing metric_type=status tag: tags=%v", dp.Tags)
			}
		}
	}
	if !foundCheck {
		t.Error("check result datapoint missing on successful collection")
	}
}

func TestCollectKeysCheckResultUnreachable(t *testing.T) {
	probe, err := NewPHPFPMProbe(map[string]interface{}{
		"endpoint":      "http://127.0.0.1:19999/fpm-status",
		"instance_name": "phpfpm-test-instance",
	}, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewPHPFPMProbe() error = %v", err)
	}

	points, _ := probe.Collect()

	foundUp := false
	foundCheck := false
	for _, dp := range points {
		if !hasTag(dp, "service.instance.id", "phpfpm-test-instance") {
			t.Errorf("datapoint %q missing/incorrect service.instance.id tag: tags=%v", dp.Name, dp.Tags)
		}
		switch dp.Name {
		case "senhub.phpfpm.up":
			foundUp = true
			if dp.Value != 0 {
				t.Errorf("senhub.phpfpm.up = %v, want 0", dp.Value)
			}
			if !hasTag(dp, "metric_type", "availability") {
				t.Errorf("senhub.phpfpm.up missing metric_type=availability tag: tags=%v", dp.Tags)
			}
		case dbcommon.CheckResultMetric:
			foundCheck = true
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("check result = %v, want %d", dp.Value, dbcommon.CheckTargetDown)
			}
			if !hasTag(dp, "metric_type", "status") {
				t.Errorf("check result missing metric_type=status tag: tags=%v", dp.Tags)
			}
		}
	}
	if !foundUp {
		t.Error("senhub.phpfpm.up missing on unreachable endpoint")
	}
	if !foundCheck {
		t.Error("check result datapoint missing on unreachable endpoint")
	}
}
