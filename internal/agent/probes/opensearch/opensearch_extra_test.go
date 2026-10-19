package opensearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

func extraPointValue(t *testing.T, points []data_store.DataPoint, name string) float64 {
	t.Helper()
	for _, dp := range points {
		if dp.Name == name {
			return dp.Value
		}
	}
	t.Fatalf("datapoint %q not found", name)
	return 0
}

func extraPointExists(t *testing.T, points []data_store.DataPoint, name string) bool {
	t.Helper()
	for _, dp := range points {
		if dp.Name == name {
			return true
		}
	}
	return false
}

func TestEntitySource_SetPinnedID_DoesNotChangeOncePinned(t *testing.T) {
	src := newOpensearchEntitySource("localhost", 9200, "")

	src.setPinnedID("")
	if src.isPinned() {
		t.Fatal("entity id must not be pinned by an empty cluster UUID")
	}

	src.setPinnedID("first-uuid")
	want := "opensearch:first-uuid"
	if got := src.pinnedID; got != want {
		t.Fatalf("pinnedID after first set = %q, want %q", got, want)
	}

	src.setPinnedID("second-uuid")
	if got := src.pinnedID; got != want {
		t.Errorf("pinnedID after second set = %q, want unchanged %q", got, want)
	}
}

func TestEntitySource_SetPinnedID_DoesNotOverrideInstanceName(t *testing.T) {
	src := newOpensearchEntitySource("localhost", 9200, "custom-name")

	src.setPinnedID("cluster-uuid")
	if got := src.pinnedID; got != "custom-name" {
		t.Errorf("pinnedID = %q, want instance_name override %q", got, "custom-name")
	}
}

func TestHTTPStatusError_ErrorString(t *testing.T) {
	err := &httpStatusError{
		statusCode: http.StatusUnauthorized,
		url:        "http://opensearch:9200/_cluster/health",
		body:       "Unauthorized",
	}
	want := "GET http://opensearch:9200/_cluster/health returned 401: Unauthorized"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestFetchNodeStats_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "green"}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			http.Error(w, "node stats unavailable", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1 because cluster health succeeded", got)
	}
	check := extraPointValue(t, points, dbcommon.CheckResultMetric)
	want := float64(dbcommon.ClassifyHTTPStatus(http.StatusInternalServerError))
	if check != want {
		t.Errorf("check result = %v, want %v", check, want)
	}
	if extraPointExists(t, points, "opensearch.process.cpu.usage") {
		t.Error("node stats metric emitted despite fetch failure")
	}
}

func TestFetchNodeStats_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "green"}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			if _, err := w.Write([]byte("{invalid-json")); err != nil {
				t.Errorf("write malformed json: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	_, fetchErr := op.fetchNodeStats()
	if fetchErr == nil {
		t.Fatal("fetchNodeStats() should fail on malformed JSON")
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1 because cluster health succeeded", got)
	}
	check := extraPointValue(t, points, dbcommon.CheckResultMetric)
	want := float64(dbcommon.ClassifyError(fetchErr))
	if check != want {
		t.Errorf("check result = %v, want %v", check, want)
	}
}

func TestFetchNodeStats_EmptyNodesMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "green"}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			if _, err := w.Write([]byte(`{"nodes":{}}`)); err != nil {
				t.Errorf("write empty nodes: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1 when cluster health succeeds", got)
	}
	check := extraPointValue(t, points, dbcommon.CheckResultMetric)
	want := float64(dbcommon.ClassifyError(nil))
	if check != want {
		t.Errorf("check result = %v, want %v for nil error", check, want)
	}
	if extraPointExists(t, points, "opensearch.process.cpu.usage") {
		t.Error("node stats metric emitted for empty nodes map")
	}
}

func TestBuildNodeStatsPoints_MultipleNodesProcessesOnlyFirst(t *testing.T) {
	p, err := NewOpenSearchProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)

	resp := nodeStatsResponse{
		Nodes: map[string]nodeStats{
			"node-a": {
				Name: "node-a",
				Process: processStats{CPU: struct {
					Percent int `json:"percent"`
				}{Percent: 11}},
			},
			"node-b": {
				Name: "node-b",
				Process: processStats{CPU: struct {
					Percent int `json:"percent"`
				}{Percent: 22}},
			},
		},
	}

	points := op.buildNodeStatsPoints(&resp, time.Now(), nil)
	count := 0
	for _, dp := range points {
		if dp.Name == "opensearch.process.cpu.usage" {
			count++
			if dp.Value != 11 && dp.Value != 22 {
				t.Errorf("opensearch.process.cpu.usage = %v, want either 11 or 22 (map order is random)", dp.Value)
			}
		}
	}
	if count != 1 {
		t.Errorf("opensearch.process.cpu.usage emitted %d times, want 1 for multiple nodes break", count)
	}
}

func TestCollect_ClusterRedStatusMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "red", NumberOfNodes: 1}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			if _, err := w.Write([]byte(`{"nodes":{}}`)); err != nil {
				t.Errorf("write empty nodes: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "opensearch.cluster.health"); got != 0 {
		t.Errorf("opensearch.cluster.health = %v, want 0 for red status", got)
	}
	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1 when cluster is reachable", got)
	}
}

func TestCollect_AuthFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"unauthorized 401", http.StatusUnauthorized},
		{"forbidden 403", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tc.name, tc.status)
			}))
			defer srv.Close()

			p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
			if err != nil {
				t.Fatalf("NewOpenSearchProbe: %v", err)
			}
			op := p.(*opensearchProbe)
			op.client = srv.Client()

			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 0 {
				t.Errorf("senhub.opensearch.up = %v, want 0", got)
			}
			check := extraPointValue(t, points, dbcommon.CheckResultMetric)
			want := float64(dbcommon.ClassifyHTTPStatus(tc.status))
			if check != want {
				t.Errorf("check result = %v, want %v", check, want)
			}
		})
	}
}

func TestCollect_TLSVerifyFailureWithDefaultClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "green"}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			if _, err := w.Write([]byte(`{"nodes":{}}`)); err != nil {
				t.Errorf("write empty nodes: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)

	var health clusterHealth
	tlsErr := op.getJSON("/_cluster/health", &health)
	if tlsErr == nil {
		t.Fatal("expected TLS verification error against self-signed test certificate")
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 0 {
		t.Errorf("senhub.opensearch.up = %v, want 0 on TLS verification failure", got)
	}
	check := extraPointValue(t, points, dbcommon.CheckResultMetric)
	want := float64(dbcommon.ClassifyError(tlsErr))
	if check != want {
		t.Errorf("check result = %v, want %v", check, want)
	}
}

func TestCollect_TLSVerifySuccessWithTrustingClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			if err := json.NewEncoder(w).Encode(clusterHealth{Status: "green"}); err != nil {
				t.Errorf("encode health: %v", err)
			}
		case "/_nodes/_local/stats":
			if _, err := w.Write([]byte(`{"nodes":{}}`)); err != nil {
				t.Errorf("write empty nodes: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{"endpoint": srv.URL}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client() // trusts the test server certificate

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := extraPointValue(t, points, "senhub.opensearch.up"); got != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1 when TLS certificate is trusted", got)
	}
	if got := extraPointValue(t, points, "opensearch.cluster.health"); got != 2 {
		t.Errorf("opensearch.cluster.health = %v, want 2 for green status", got)
	}
}

func TestOpensearchProbe_GetTargetStrategies(t *testing.T) {
	p, err := NewOpenSearchProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}

	got := p.(*opensearchProbe).GetTargetStrategies()
	want := []string{"senhub", "prtg", "http", "otlp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetTargetStrategies() = %v, want %v", got, want)
	}
}

func TestOpensearchProbe_OnStartAndOnShutdown(t *testing.T) {
	p, err := NewOpenSearchProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)

	if err := op.OnStart(make(chan struct{})); err != nil {
		t.Errorf("OnStart returned error: %v", err)
	}
	if err := op.OnShutdown(context.Background()); err != nil {
		t.Errorf("OnShutdown returned error: %v", err)
	}
}
