package cassandra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// jolokiaFixture is a minimal Jolokia HTTP fixture.
// It maps "mbean/attribute" -> raw JSON value.
type jolokiaFixture struct {
	responses map[string]interface{}
}

func (f *jolokiaFixture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// URL pattern: /jolokia/read/<mbean>/<attribute>
		// We match on the path suffix after /jolokia/read/
		path := r.URL.Path
		const prefix = "/jolokia/read/"
		if len(path) <= len(prefix) {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		key := path[len(prefix):]

		val, ok := f.responses[key]
		if !ok {
			// Return a Jolokia error response
			body := `{"status":500,"error":"MBean not found: ` + key + `","value":null}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK) // Jolokia always 200 at HTTP level
			_, _ = w.Write([]byte(body))
			return
		}

		raw, err := json.Marshal(val)
		if err != nil {
			http.Error(w, "marshal error", http.StatusInternalServerError)
			return
		}
		resp := map[string]interface{}{
			"status": 200,
			"value":  json.RawMessage(raw),
		}
		out, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}
}

// defaultFixture builds a complete set of Jolokia responses for the happy path.
func defaultFixture() *jolokiaFixture {
	return &jolokiaFixture{
		responses: map[string]interface{}{
			// Client connections
			"org.apache.cassandra.metrics:type=Client,name=connectedNativeClients/Value": 42,

			// Read latency
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency/Count":          int64(10000),
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency/Mean":           float64(500.0), // µs
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency/99thPercentile": float64(2000.0),

			// Write latency
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency/Count":          int64(20000),
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency/Mean":           float64(300.0),
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency/99thPercentile": float64(1500.0),

			// Read errors
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Errors/Count": int64(5),

			// Write errors
			"org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Errors/Count": int64(2),

			// Compaction
			"org.apache.cassandra.metrics:type=Compaction,name=CompletedTasks/Value": int64(1234),
			"org.apache.cassandra.metrics:type=Compaction,name=PendingTasks/Value":   int64(3),

			// Storage
			"org.apache.cassandra.metrics:type=Storage,name=Load/Count":       int64(5368709120), // 5 GiB
			"org.apache.cassandra.metrics:type=Storage,name=TotalHints/Count": int64(100),

			// JVM memory
			"java.lang:type=Memory/HeapMemoryUsage": map[string]interface{}{
				"used":      json.Number("2147483648"),
				"committed": json.Number("4294967296"),
				"init":      json.Number("536870912"),
				"max":       json.Number("8589934592"),
			},

			// GC (wildcard returns map keyed by mbean object name)
			"java.lang:type=GarbageCollector,name=*/CollectionCount,CollectionTime": map[string]map[string]json.Number{
				"java.lang:name=G1 Young Generation,type=GarbageCollector": {
					"CollectionCount": json.Number("150"),
					"CollectionTime":  json.Number("3200"),
				},
				"java.lang:name=G1 Old Generation,type=GarbageCollector": {
					"CollectionCount": json.Number("2"),
					"CollectionTime":  json.Number("450"),
				},
			},
		},
	}
}

func newTestProbe(t *testing.T, jolokiaURL string) *cassandraProbe {
	t.Helper()
	cfg := map[string]interface{}{
		"jolokia_url": jolokiaURL,
		"timeout":     5,
	}
	probe, err := NewcassandraProbe(cfg, testBaseLogger())
	if err != nil {
		t.Fatalf("NewcassandraProbe: %v", err)
	}
	p, ok := probe.(*cassandraProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("cassandra-test")
	return p
}

func TestNewcassandraProbe_Defaults(t *testing.T) {
	probe, err := NewcassandraProbe(map[string]interface{}{}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewcassandraProbe() error = %v", err)
	}
	if probe == nil {
		t.Fatal("NewcassandraProbe() returned nil")
	}
	p, ok := probe.(*cassandraProbe)
	if !ok {
		t.Fatal("NewcassandraProbe() returned unexpected type")
	}
	if p.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", p.GetProbeType(), ProbeType)
	}
}

func TestCollect_HappyPath(t *testing.T) {
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	// up must be 1 on success
	if byName["senhub.cassandra.up"] != 1 {
		t.Errorf("up = %v, want 1", byName["senhub.cassandra.up"])
	}

	// connections
	if byName["cassandra.client.connections"] != 42 {
		t.Errorf("connections = %v, want 42", byName["cassandra.client.connections"])
	}

	// storage load (5 GiB)
	if byName["cassandra.storage.load"] != float32(5368709120) {
		t.Errorf("storage.load = %v, want 5368709120", byName["cassandra.storage.load"])
	}

	// compaction pending
	if byName["cassandra.compaction.tasks.pending"] != 3 {
		t.Errorf("compaction.pending = %v, want 3", byName["cassandra.compaction.tasks.pending"])
	}

	// heap used (2 GiB)
	if byName["jvm.memory.heap.used"] != float32(2147483648) {
		t.Errorf("heap.used = %v, want 2147483648", byName["jvm.memory.heap.used"])
	}

	// GC metrics should be present (2 collectors x 2 metrics = 4 GC points)
	gcCount := 0
	gcElapsed := 0
	for _, dp := range points {
		switch dp.Name {
		case "jvm.gc.collections.count":
			gcCount++
		case "jvm.gc.collections.elapsed":
			gcElapsed++
		}
	}
	if gcCount != 2 {
		t.Errorf("jvm.gc.collections.count emitted %d times, want 2", gcCount)
	}
	if gcElapsed != 2 {
		t.Errorf("jvm.gc.collections.elapsed emitted %d times, want 2", gcElapsed)
	}
}

func TestCollect_OperationMetrics(t *testing.T) {
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Verify per-operation tags
	readCount := 0
	writeCount := 0
	for _, dp := range points {
		if dp.Name != "cassandra.client.requests.count" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "operation" {
				switch tg.Value {
				case "read":
					readCount++
				case "write":
					writeCount++
				}
			}
		}
	}
	if readCount != 1 {
		t.Errorf("read request count datapoints = %d, want 1", readCount)
	}
	if writeCount != 1 {
		t.Errorf("write request count datapoints = %d, want 1", writeCount)
	}
}

func TestCollect_UpZeroOnFailure(t *testing.T) {
	// Point to a non-existent server so the first MBean read fails.
	p := newTestProbe(t, "http://127.0.0.1:1/jolokia")

	points, err := p.Collect()
	// Collect must return nil error even when Cassandra is unreachable.
	if err != nil {
		t.Fatalf("Collect() must not return an error on connection failure, got %v", err)
	}

	// Find the up datapoint
	for _, dp := range points {
		if dp.Name == "senhub.cassandra.up" {
			if dp.Value != 0 {
				t.Errorf("up = %v, want 0 when Cassandra unreachable", dp.Value)
			}
			return
		}
	}
	t.Fatal("senhub.cassandra.up not found in datapoints")
}

func TestCollect_AllPointsHaveMetricTypeTag(t *testing.T) {
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, dp := range points {
		hasMetricType := false
		for _, tg := range dp.Tags {
			if tg.Key == "metric_type" && tg.Value != "" {
				hasMetricType = true
				break
			}
		}
		if !hasMetricType {
			t.Errorf("datapoint %q missing metric_type tag", dp.Name)
		}
	}
}

func TestCollect_ProbeNameEnriched(t *testing.T) {
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, dp := range points {
		hasProbeName := false
		for _, tg := range dp.Tags {
			if tg.Key == "probe_name" {
				hasProbeName = true
				break
			}
		}
		if !hasProbeName {
			t.Errorf("datapoint %q missing probe_name tag (EnrichDataPointsWithProbeName not called)", dp.Name)
		}
	}
}

func TestExtractGCName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"java.lang:name=G1 Young Generation,type=GarbageCollector", "G1 Young Generation"},
		{"java.lang:name=ConcurrentMarkSweep,type=GarbageCollector", "ConcurrentMarkSweep"},
		{"java.lang:type=GarbageCollector,name=ParNew", "ParNew"},
		{"no_name_here", "no_name_here"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := extractGCName(tc.input)
			if got != tc.want {
				t.Errorf("extractGCName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLatencyConversionToMs(t *testing.T) {
	// Mean = 500 µs -> 0.5 ms
	fix := defaultFixture()
	srv := httptest.NewServer(fix.handler())
	defer srv.Close()

	p := newTestProbe(t, srv.URL+"/jolokia")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name != "cassandra.client.requests.latency" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "operation" && tg.Value == "read" {
				// 500 µs / 1000 = 0.5 ms
				if fmt.Sprintf("%.4f", dp.Value) != "0.5000" {
					t.Errorf("read latency mean = %v ms, want 0.5 ms (500 µs)", dp.Value)
				}
			}
		}
	}
}
