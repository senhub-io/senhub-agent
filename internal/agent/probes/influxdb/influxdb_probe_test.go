package influxdb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *InfluxDBProbe {
	t.Helper()
	probe, err := NewInfluxDBProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewInfluxDBProbe: %v", err)
	}
	p, ok := probe.(*InfluxDBProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("influxdb-test")
	return p
}

// TestParseConfig_Defaults verifies that omitting optional fields uses
// the documented defaults.
func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", cfg.Endpoint, defaultEndpoint)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", cfg.Timeout, defaultTimeout)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", cfg.Interval, defaultInterval)
	}
}

// TestParseConfig_Override verifies that provided values override defaults.
func TestParseConfig_Override(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint": "http://myhost:8086",
		"token":    "my-token",
		"org":      "myorg",
		"timeout":  30,
		"interval": 120,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Endpoint != "http://myhost:8086" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Token != "my-token" {
		t.Errorf("token = %q", cfg.Token)
	}
	if cfg.Org != "myorg" {
		t.Errorf("org = %q", cfg.Org)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
	if cfg.Interval != 120*time.Second {
		t.Errorf("interval = %v", cfg.Interval)
	}
}

// TestParseConfig_TrailingSlashStripped ensures the endpoint never ends
// with a slash (which would produce double-slash in URL construction).
func TestParseConfig_TrailingSlashStripped(t *testing.T) {
	cfg, _ := parseConfig(map[string]interface{}{"endpoint": "http://host:8086/"})
	if strings.HasSuffix(cfg.Endpoint, "/") {
		t.Errorf("endpoint should not end with /: %q", cfg.Endpoint)
	}
}

// TestParsePrometheusText verifies the Prometheus text parser handles
// the formats that InfluxDB actually emits.
func TestParsePrometheusText(t *testing.T) {
	input := `# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42
# HELP go_memstats_heap_inuse_bytes Number of heap bytes in-use spans.
# TYPE go_memstats_heap_inuse_bytes gauge
go_memstats_heap_inuse_bytes 1.23456789e+07
# HELP storage_reads_total Total number of storage reads.
# TYPE storage_reads_total counter
storage_reads_total{quantile="0.5"} 100
storage_reads_total{quantile="0.9"} 200
`

	result := parsePrometheusText(strings.NewReader(input))

	if v, ok := result["go_goroutines"]; !ok || v != 42 {
		t.Errorf("go_goroutines: got %v, want 42", v)
	}
	if v, ok := result["go_memstats_heap_inuse_bytes"]; !ok || v == 0 {
		t.Errorf("go_memstats_heap_inuse_bytes missing or zero: %v", v)
	}
	// Label sets: only the first occurrence should be recorded.
	if v, ok := result["storage_reads_total"]; !ok || v != 100 {
		t.Errorf("storage_reads_total: got %v (want 100, first-seen wins)", v)
	}
}

// TestParsePromLine exercises the line parser directly.
func TestParsePromLine(t *testing.T) {
	cases := []struct {
		line      string
		wantName  string
		wantValue float64
		wantOK    bool
	}{
		{"go_goroutines 42", "go_goroutines", 42, true},
		{"go_goroutines{} 42", "go_goroutines", 42, true},
		{"metric{a=\"b\",c=\"d\"} 3.14 1234567890", "metric", 3.14, true},
		{"# HELP foo bar", "", 0, false},
		{"# TYPE foo gauge", "", 0, false},
		{"", "", 0, false},
		{"only_name", "", 0, false},
	}
	for _, tc := range cases {
		name, value, ok := parsePromLine(tc.line)
		if ok != tc.wantOK {
			t.Errorf("parsePromLine(%q): ok=%v want %v", tc.line, ok, tc.wantOK)
			continue
		}
		if !tc.wantOK {
			continue
		}
		if name != tc.wantName {
			t.Errorf("parsePromLine(%q): name=%q want %q", tc.line, name, tc.wantName)
		}
		if value != tc.wantValue {
			t.Errorf("parsePromLine(%q): value=%v want %v", tc.line, value, tc.wantValue)
		}
	}
}

// TestCollect_UpAndMetrics runs a full Collect() cycle against a fake
// InfluxDB server and validates the key datapoints.
func TestCollect_UpAndMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"pass","name":"influxdb","version":"2.7.0"}`))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("go_goroutines 55\ngo_memstats_heap_inuse_bytes 8388608\nstorage_reads_total 10\nstorage_writes_total 20\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if v, ok := byName["senhub.influxdb.up"]; !ok || v != 1 {
		t.Errorf("senhub.influxdb.up = %v, want 1", v)
	}
	if v, ok := byName["go.goroutines"]; !ok || v != 55 {
		t.Errorf("go.goroutines = %v, want 55", v)
	}
	if v, ok := byName["go.memory.heap.used"]; !ok || v == 0 {
		t.Errorf("go.memory.heap.used missing or zero: %v", v)
	}
	if v, ok := byName["influxdb.storage.reads"]; !ok || v != 10 {
		t.Errorf("influxdb.storage.reads = %v, want 10", v)
	}
	if v, ok := byName["influxdb.storage.writes"]; !ok || v != 20 {
		t.Errorf("influxdb.storage.writes = %v, want 20", v)
	}
}

// TestCollect_Up0_WhenHealthFails verifies that a down server produces
// up=0 without a collection error.
func TestCollect_Up0_WhenHealthFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should not return error on unhealthy server: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.influxdb.up" {
			if dp.Value != 0 {
				t.Errorf("senhub.influxdb.up = %v, want 0 when server is down", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.influxdb.up not found in output")
}

// TestCollect_ProbeName checks that EnrichDataPointsWithProbeName ran.
func TestCollect_ProbeName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"pass"}`))
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints returned")
	}
	for _, dp := range points {
		hasProbeType := false
		for _, tg := range dp.Tags {
			if tg.Key == "probe_type" && tg.Value == ProbeType {
				hasProbeType = true
			}
		}
		if !hasProbeType {
			t.Errorf("datapoint %q missing probe_type=%q tag", dp.Name, ProbeType)
		}
	}
}

// TestCollect_BucketsWithToken checks that the bucket count is emitted
// when a token is configured.
func TestCollect_BucketsWithToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"pass"}`))
		case "/api/v2/buckets":
			if r.Header.Get("Authorization") == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"buckets":[{},{},{}]}`))
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"endpoint": srv.URL,
		"token":    "test-token",
	})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "influxdb.buckets" {
			if dp.Value != 3 {
				t.Errorf("influxdb.buckets = %v, want 3", dp.Value)
			}
			return
		}
	}
	t.Error("influxdb.buckets not found in output")
}
