package elasticsearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func findPoint(points []data_store.DataPoint, name string) *data_store.DataPoint {
	for i := range points {
		if points[i].Name == name {
			return &points[i]
		}
	}
	return nil
}

func hasTag(dp data_store.DataPoint, key, want string) bool {
	for _, tag := range dp.Tags {
		if tag.Key == key {
			return tag.Value == want
		}
	}
	return want == ""
}

func TestCollect_Success_AllDatapointsCarryEntityIdentityAndCheckOK(t *testing.T) {
	const wantUUID = "success-test-cluster-uuid"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"cluster_uuid": wantUUID,
				"version":      map[string]interface{}{"number": "8.0.0"},
			})
		case "/_cluster/health":
			_ = json.NewEncoder(w).Encode(clusterHealth{Status: "green"})
		case "/_nodes/_local/stats":
			_ = json.NewEncoder(w).Encode(nodeStatsResponse{Nodes: map[string]nodeStats{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewElasticsearchProbe() error: %v", err)
	}
	ep := p.(*elasticsearchProbe)
	ep.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	wantID := "elasticsearch:" + wantUUID
	if got := ep.entitySrc.currentInstanceID(); got != wantID {
		t.Fatalf("entity source current db.instance.id = %q, want %q", got, wantID)
	}

	for _, dp := range points {
		if !hasTag(dp, "db.instance.id", wantID) {
			t.Errorf("datapoint %q is missing db.instance.id=%q: tags=%v", dp.Name, wantID, dp.Tags)
		}
	}

	check := findPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("check result datapoint %q not found", dbcommon.CheckResultMetric)
	}
	if check.Value != 0 {
		t.Errorf("check result value = %v, want 0", check.Value)
	}
	if !hasTag(*check, "metric_type", "status") {
		t.Errorf("check result datapoint is missing metric_type=status: tags=%v", check.Tags)
	}
}

func TestCollect_Unreachable_DatapointsCarryFallbackIdentityAndCheckClassified(t *testing.T) {
	p, err := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999",
		"timeout":  1,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewElasticsearchProbe() error: %v", err)
	}
	ep := p.(*elasticsearchProbe)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error for unreachable host, got: %v", err)
	}

	wantID := ep.entitySrc.currentInstanceID()
	if wantID != "" {
		t.Fatalf("id %q before any entity was emitted, want none", wantID)
	}

	for _, dp := range points {
		if !hasTag(dp, "db.instance.id", wantID) {
			t.Errorf("datapoint %q is missing db.instance.id=%q: tags=%v", dp.Name, wantID, dp.Tags)
		}
	}

	up := findPoint(points, "senhub.elasticsearch.up")
	if up == nil {
		t.Fatal("senhub.elasticsearch.up metric not found")
	}
	if up.Value != 0 {
		t.Errorf("senhub.elasticsearch.up = %v, want 0 when down", up.Value)
	}

	check := findPoint(points, dbcommon.CheckResultMetric)
	if check == nil {
		t.Fatalf("check result datapoint %q not found", dbcommon.CheckResultMetric)
	}
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result value = %v, want %v", check.Value, float64(dbcommon.CheckTargetDown))
	}
	if !hasTag(*check, "metric_type", "status") {
		t.Errorf("check result datapoint is missing metric_type=status: tags=%v", check.Tags)
	}
}
