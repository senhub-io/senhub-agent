package elasticsearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestNewElasticsearchProbe_Defaults(t *testing.T) {
	p, err := NewElasticsearchProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := p.(*elasticsearchProbe)
	if ep.cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", ep.cfg.Endpoint, defaultEndpoint)
	}
	if ep.cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", ep.cfg.Interval, defaultInterval)
	}
	if ep.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", ep.cfg.Timeout, defaultTimeout)
	}
}

func TestNewElasticsearchProbe_CustomConfig(t *testing.T) {
	p, err := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": "http://es:9200",
		"username": "admin",
		"password": "secret",
		"interval": 30,
		"timeout":  5,
	}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := p.(*elasticsearchProbe)
	if ep.cfg.Endpoint != "http://es:9200" {
		t.Errorf("endpoint = %q, want http://es:9200", ep.cfg.Endpoint)
	}
	if ep.cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", ep.cfg.Interval)
	}
	if ep.cfg.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", ep.cfg.Timeout)
	}
	if ep.cfg.Username != "admin" {
		t.Errorf("username = %q, want admin", ep.cfg.Username)
	}
}

func TestCollect_Up(t *testing.T) {
	healthResp := clusterHealth{
		Status:               "green",
		NumberOfNodes:        3,
		NumberOfDataNodes:    2,
		ActiveShards:         10,
		UnassignedShards:     0,
		RelocatingShards:     0,
		NumberOfPendingTasks: 0,
	}
	nodeResp := nodeStatsResponse{
		Nodes: map[string]nodeStats{
			"nodeA": {
				Name: "nodeA",
				JVM: jvmStats{
					Mem: struct {
						HeapUsedInBytes int64 `json:"heap_used_in_bytes"`
						HeapMaxInBytes  int64 `json:"heap_max_in_bytes"`
					}{HeapUsedInBytes: 512 * 1024 * 1024, HeapMaxInBytes: 1024 * 1024 * 1024},
					GC: struct {
						Collectors map[string]gcCollector `json:"collectors"`
					}{Collectors: map[string]gcCollector{
						"young": {CollectionCount: 100, CollectionTimeInMillis: 2000},
						"old":   {CollectionCount: 5, CollectionTimeInMillis: 500},
					}},
				},
				Process: processStats{CPU: struct {
					Percent int `json:"percent"`
				}{Percent: 42}},
				OS: osStats{Mem: struct {
					UsedInBytes int64 `json:"used_in_bytes"`
				}{UsedInBytes: 2 * 1024 * 1024 * 1024}},
				Indices: indexStats{
					Indexing: struct {
						IndexTotal        int64 `json:"index_total"`
						IndexTimeInMillis int64 `json:"index_time_in_millis"`
					}{IndexTotal: 1000, IndexTimeInMillis: 5000},
					Search: struct {
						QueryTotal        int64 `json:"query_total"`
						QueryTimeInMillis int64 `json:"query_time_in_millis"`
						FetchTotal        int64 `json:"fetch_total"`
						FetchTimeInMillis int64 `json:"fetch_time_in_millis"`
					}{QueryTotal: 500, QueryTimeInMillis: 2000, FetchTotal: 450, FetchTimeInMillis: 1000},
				},
				ThreadPool: map[string]threadPoolStats{
					"search": {Queue: 5, Completed: 10000, Rejected: 2},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			_ = json.NewEncoder(w).Encode(healthResp)
		case "/_nodes/_local/stats":
			_ = json.NewEncoder(w).Encode(nodeResp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, _ := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, testLogger())
	ep := p.(*elasticsearchProbe)
	ep.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.elasticsearch.up"] != 1 {
		t.Errorf("senhub.elasticsearch.up = %v, want 1", byName["senhub.elasticsearch.up"])
	}
	if byName["elasticsearch.cluster.health"] != 2 {
		t.Errorf("elasticsearch.cluster.health = %v, want 2 (green)", byName["elasticsearch.cluster.health"])
	}
	if byName["elasticsearch.cluster.nodes"] != 3 {
		t.Errorf("elasticsearch.cluster.nodes = %v, want 3", byName["elasticsearch.cluster.nodes"])
	}
	if byName["elasticsearch.jvm.memory.heap.used"] != float32(512*1024*1024) {
		t.Errorf("elasticsearch.jvm.memory.heap.used = %v, want %v", byName["elasticsearch.jvm.memory.heap.used"], float32(512*1024*1024))
	}
	if byName["elasticsearch.process.cpu.usage"] != 0.42 {
		t.Errorf("elasticsearch.process.cpu.usage = %v, want 0.42", byName["elasticsearch.process.cpu.usage"])
	}
}

func TestCollect_Down(t *testing.T) {
	p, _ := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999", // nothing listening
		"timeout":  1,
	}, testLogger())

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error for unreachable host, got: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.elasticsearch.up" {
			if dp.Value != 0 {
				t.Errorf("senhub.elasticsearch.up = %v, want 0 when down", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.elasticsearch.up metric not found in points")
}

func TestStatusToFloat(t *testing.T) {
	cases := []struct {
		status string
		want   float32
	}{
		{"green", 2},
		{"yellow", 1},
		{"red", 0},
		{"unknown", 0},
	}
	for _, tc := range cases {
		got := statusToFloat(tc.status)
		if got != tc.want {
			t.Errorf("statusToFloat(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestGetInterval(t *testing.T) {
	p, _ := NewElasticsearchProbe(map[string]interface{}{"interval": 120}, testLogger())
	if p.GetInterval() != 120*time.Second {
		t.Errorf("GetInterval() = %v, want 120s", p.GetInterval())
	}
}

func TestShouldStart(t *testing.T) {
	p, _ := NewElasticsearchProbe(map[string]interface{}{}, testLogger())
	if !p.ShouldStart() {
		t.Error("ShouldStart() = false, want true")
	}
}

func TestCollect_BasicAuth(t *testing.T) {
	authOK := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		authOK = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cluster/health":
			_ = json.NewEncoder(w).Encode(clusterHealth{Status: "green"})
		case "/_nodes/_local/stats":
			_ = json.NewEncoder(w).Encode(nodeStatsResponse{Nodes: map[string]nodeStats{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, _ := NewElasticsearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "admin",
		"password": "secret",
	}, testLogger())
	ep := p.(*elasticsearchProbe)
	ep.client = srv.Client()

	_, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if !authOK {
		t.Error("basic auth credentials were not used by the probe")
	}
}
