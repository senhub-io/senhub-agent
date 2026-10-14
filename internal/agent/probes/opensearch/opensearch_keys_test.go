package opensearch

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
)

func getTag(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func TestCollect_AllPointsCarryEntityIDAndCheckResultOK(t *testing.T) {
	const clusterUUID = "uuid-check-ok"
	srv := testServer(t, clusterUUID)
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect(): %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	wantID := "opensearch:" + clusterUUID
	var foundCheck bool
	for _, dp := range points {
		if got := getTag(dp.Tags, "db.instance.id"); got != wantID {
			t.Errorf("%s: db.instance.id tag = %q, want %q", dp.Name, got, wantID)
		}

		if dp.Name == dbcommon.CheckResultMetric {
			foundCheck = true
			if dp.Value != float64(dbcommon.CheckOK) {
				t.Errorf("%s = %v, want 0", dp.Name, dp.Value)
			}
			if got := getTag(dp.Tags, "metric_type"); got != "status" {
				t.Errorf("%s metric_type = %q, want status", dp.Name, got)
			}
		}
	}

	if !foundCheck {
		t.Fatalf("%s metric not found", dbcommon.CheckResultMetric)
	}
}

func TestCollect_Down_CheckResultFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := srv.URL
	parsed, err := url.Parse(endpoint)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	_ = parsed
	srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": endpoint,
		"timeout":  1,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error for unreachable host, got: %v", err)
	}

	var foundUp, foundCheck bool
	for _, dp := range points {
		if got := getTag(dp.Tags, "db.instance.id"); got != "" {
			t.Errorf("%s: db.instance.id tag = %q before any entity, want none", dp.Name, got)
		}

		switch dp.Name {
		case "senhub.opensearch.up":
			foundUp = true
			if dp.Value != 0 {
				t.Errorf("senhub.opensearch.up = %v, want 0 when down", dp.Value)
			}
		case dbcommon.CheckResultMetric:
			foundCheck = true
			if dp.Value != float64(dbcommon.CheckTargetDown) {
				t.Errorf("%s = %v, want %v", dp.Name, dp.Value, float64(dbcommon.CheckTargetDown))
			}
			if got := getTag(dp.Tags, "metric_type"); got != "status" {
				t.Errorf("%s metric_type = %q, want status", dp.Name, got)
			}
		}
	}

	if !foundUp {
		t.Error("senhub.opensearch.up metric not found")
	}
	if !foundCheck {
		t.Errorf("%s metric not found", dbcommon.CheckResultMetric)
	}
}
