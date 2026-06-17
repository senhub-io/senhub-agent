package opensearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestNewOpenSearchProbe_Defaults(t *testing.T) {
	p, err := NewOpenSearchProbe(map[string]interface{}{}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	op := p.(*opensearchProbe)
	if op.cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", op.cfg.Endpoint, defaultEndpoint)
	}
	if op.cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", op.cfg.Interval, defaultInterval)
	}
	if op.cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", op.cfg.Timeout, defaultTimeout)
	}
}

func TestNewOpenSearchProbe_CustomConfig(t *testing.T) {
	p, err := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": "http://opensearch:9200",
		"username": "admin",
		"password": "secret",
		"interval": 30,
		"timeout":  5,
	}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	op := p.(*opensearchProbe)
	if op.cfg.Endpoint != "http://opensearch:9200" {
		t.Errorf("endpoint = %q, want http://opensearch:9200", op.cfg.Endpoint)
	}
	if op.cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", op.cfg.Interval)
	}
	if op.cfg.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", op.cfg.Timeout)
	}
	if op.cfg.Username != "admin" {
		t.Errorf("username = %q, want admin", op.cfg.Username)
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

	p, _ := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, testLogger())
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	byName := map[string]float64{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.opensearch.up"] != 1 {
		t.Errorf("senhub.opensearch.up = %v, want 1", byName["senhub.opensearch.up"])
	}
	if byName["opensearch.cluster.health"] != 2 {
		t.Errorf("opensearch.cluster.health = %v, want 2 (green)", byName["opensearch.cluster.health"])
	}
	if byName["opensearch.cluster.nodes"] != 3 {
		t.Errorf("opensearch.cluster.nodes = %v, want 3", byName["opensearch.cluster.nodes"])
	}
	if byName["opensearch.jvm.memory.heap.used"] != float64(512*1024*1024) {
		t.Errorf("opensearch.jvm.memory.heap.used = %v, want %v", byName["opensearch.jvm.memory.heap.used"], float64(512*1024*1024))
	}
	if byName["opensearch.process.cpu.usage"] != 0.42 {
		t.Errorf("opensearch.process.cpu.usage = %v, want 0.42", byName["opensearch.process.cpu.usage"])
	}
}

func TestCollect_Down(t *testing.T) {
	p, _ := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999", // nothing listening
		"timeout":  1,
	}, testLogger())

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error for unreachable host, got: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.opensearch.up" {
			if dp.Value != 0 {
				t.Errorf("senhub.opensearch.up = %v, want 0 when down", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.opensearch.up metric not found in points")
}

func TestStatusToFloat(t *testing.T) {
	cases := []struct {
		status string
		want   float64
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
	p, _ := NewOpenSearchProbe(map[string]interface{}{"interval": 120}, testLogger())
	if p.GetInterval() != 120*time.Second {
		t.Errorf("GetInterval() = %v, want 120s", p.GetInterval())
	}
}

func TestShouldStart(t *testing.T) {
	p, _ := NewOpenSearchProbe(map[string]interface{}{}, testLogger())
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

	p, _ := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "admin",
		"password": "secret",
	}, testLogger())
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	_, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if !authOK {
		t.Error("basic auth credentials were not used by the probe")
	}
}

// ----- entity source tests ----------------------------------------------------

// testServer builds an httptest server that serves minimal OpenSearch JSON
// responses for the paths the probe fetches. clusterUUID is served at GET /;
// pass "" to omit cluster_uuid from the root response (simulates a node whose
// UUID is not yet assigned).
func testServer(t *testing.T, clusterUUID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"cluster_uuid": clusterUUID,
			})
		case "/_cluster/health":
			_ = json.NewEncoder(w).Encode(clusterHealth{Status: "green", NumberOfNodes: 1})
		case "/_nodes/_local/stats":
			_ = json.NewEncoder(w).Encode(nodeStatsResponse{Nodes: map[string]nodeStats{
				"n1": {Name: "n1", Version: "2.13.0"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestEntitySource_InstanceNameOverride verifies that an operator-supplied
// instance_name is used verbatim as db.instance.id and is pinned at
// construction — Collect need not fetch GET /.
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	srv := testServer(t, "uuid-should-be-ignored")
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{
		"endpoint":      srv.URL,
		"instance_name": "my-prod-cluster",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	// id must be pinned immediately at construction.
	if !op.entitySrc.isPinned() {
		t.Fatal("entity id should be pinned at construction when instance_name is set")
	}
	if got := op.entitySrc.pinnedID; got != "my-prod-cluster" {
		t.Errorf("pinnedID = %q, want %q", got, "my-prod-cluster")
	}

	// After a collect cycle, Observe must emit the instance_name as db.instance.id.
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	op.entitySrc.setReachable(true, "")
	obs, ok := op.entitySrc.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want true after successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe returned %d entities, want 1", len(obs.Entities))
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	if got != "my-prod-cluster" {
		t.Errorf("db.instance.id = %q, want %q", got, "my-prod-cluster")
	}
}

// TestEntitySource_TechIDPinned verifies that when no instance_name is set,
// the entity source pins "opensearch:<cluster_uuid>" on the first successful
// collect cycle.
func TestEntitySource_TechIDPinned(t *testing.T) {
	const uuid = "aabbccdd-0011-2233-4455-ffeeddccbbaa"
	srv := testServer(t, uuid)
	defer srv.Close()

	p, err := NewOpenSearchProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, testLogger())
	if err != nil {
		t.Fatalf("NewOpenSearchProbe: %v", err)
	}
	op := p.(*opensearchProbe)
	op.client = srv.Client()

	// Before any collect, id must not be pinned.
	if op.entitySrc.isPinned() {
		t.Fatal("entity id must not be pinned before the first successful collect")
	}

	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if !op.entitySrc.isPinned() {
		t.Fatal("entity id must be pinned after the first successful collect")
	}
	want := "opensearch:" + uuid
	if got := op.entitySrc.pinnedID; got != want {
		t.Errorf("pinnedID = %q, want %q", got, want)
	}
}

// TestEntitySource_NotEmittedBeforePinned verifies that Observe returns ok=false
// before the cluster_uuid has been fetched and pinned.
func TestEntitySource_NotEmittedBeforePinned(t *testing.T) {
	src := newOpensearchEntitySource("opensearch.example.com", 9200, "")
	src.setReachable(true, "2.13.0")

	_, ok := src.Observe()
	if ok {
		t.Error("Observe must return ok=false before the id is pinned")
	}
}

// TestEntitySource_MonitorsEdge_Present verifies that when agentID is set,
// Observe includes a monitors relation from service.instance to db.
func TestEntitySource_MonitorsEdge_Present(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-test-id-001")

	src := newOpensearchEntitySource("opensearch.example.com", 9200, "")
	src.setPinnedID("cluster-abc123")
	src.setReachable(true, "2.13.0")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation (monitors), got %d", len(obs.Relations))
	}
	rel := obs.Relations[0]
	if rel.Type != "monitors" {
		t.Errorf("relation type = %q, want %q", rel.Type, "monitors")
	}
	if rel.FromType != "service.instance" {
		t.Errorf("FromType = %q, want %q", rel.FromType, "service.instance")
	}
	if got, _ := rel.FromID["service.instance.id"].(string); got != "agent-test-id-001" {
		t.Errorf("FromID[service.instance.id] = %q, want %q", got, "agent-test-id-001")
	}
	if rel.ToType != "db" {
		t.Errorf("ToType = %q, want %q", rel.ToType, "db")
	}
	if got, _ := rel.ToID["db.instance.id"].(string); got != "opensearch:cluster-abc123" {
		t.Errorf("ToID[db.instance.id] = %q, want %q", got, "opensearch:cluster-abc123")
	}
}

// TestEntitySource_MonitorsEdge_Absent verifies that when agentID is empty,
// Observe does not emit a monitors relation.
func TestEntitySource_MonitorsEdge_Absent(t *testing.T) {
	// Reset agentstate so no agent id is present for this test.
	agentstate.SetAgentInstanceID("")

	src := newOpensearchEntitySource("opensearch.example.com", 9200, "")
	src.setPinnedID("cluster-xyz789")
	src.setReachable(true, "2.13.0")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected 0 relations when agentID is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_HostPortFallback_ConstructionTime verifies the host:port
// fallback path: when the cluster genuinely has no stable tech id and
// instance_name is also empty, the source can be used with a host:port pinned
// at construction (tested via the hostPort helper + direct pin for coverage).
func TestEntitySource_HostPortFallback_ConstructionTime(t *testing.T) {
	src := newOpensearchEntitySource("db.example.com", 9200, "")
	// Simulate "no stable tech id available" by pinning host:port directly.
	hp := src.hostPort()
	src.setPinnedID(hp) // externally: the probe would do this when it learns there is no UUID
	src.setReachable(true, "")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false after host:port pin")
	}
	got, _ := obs.Entities[0].ID["db.instance.id"].(string)
	// The pinned value is "opensearch:" + hostPort (as setPinnedID prefixes).
	want := "opensearch:db.example.com:9200"
	if got != want {
		t.Errorf("db.instance.id = %q, want %q", got, want)
	}
}
