package clickhouse

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// sampleMetrics is a minimal /metrics Prometheus-text payload exercising
// the three ClickHouse metric families.
const sampleMetrics = `# HELP ClickHouseMetrics_Query Number of executing queries
# TYPE ClickHouseMetrics_Query gauge
ClickHouseMetrics_Query 3
# HELP ClickHouseMetrics_Connection Number of connections to clickhouse server
# TYPE ClickHouseMetrics_Connection gauge
ClickHouseMetrics_Connection 10
# HELP ClickHouseMetrics_MemoryTracking Total amount of memory (bytes) allocated in currently executing queries
# TYPE ClickHouseMetrics_MemoryTracking gauge
ClickHouseMetrics_MemoryTracking 524288000
# HELP ClickHouseMetrics_Parts Total amount of data parts
# TYPE ClickHouseMetrics_Parts gauge
ClickHouseMetrics_Parts 42
# HELP ClickHouseMetrics_Merge Number of executing background merges
# TYPE ClickHouseMetrics_Merge gauge
ClickHouseMetrics_Merge 2
# HELP ClickHouseAsyncMetrics_Uptime Time the server has been running (in seconds)
# TYPE ClickHouseAsyncMetrics_Uptime gauge
ClickHouseAsyncMetrics_Uptime 3600
`

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig(empty) error: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, defaultEndpoint)
	}
	if cfg.Username != defaultUsername {
		t.Errorf("Username = %q, want %q", cfg.Username, defaultUsername)
	}
	if cfg.Database != defaultDatabase {
		t.Errorf("Database = %q, want %q", cfg.Database, defaultDatabase)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, defaultTimeout)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", cfg.Interval, defaultInterval)
	}
}

func TestParseConfig_Override(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"endpoint": "http://ch.example.com:8123",
		"username": "admin",
		"password": "secret",
		"database": "mydb",
		"timeout":  30,
		"interval": 120,
	})
	if err != nil {
		t.Fatalf("parseConfig error: %v", err)
	}
	if cfg.Endpoint != "http://ch.example.com:8123" {
		t.Errorf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.Username != "admin" {
		t.Errorf("Username = %q", cfg.Username)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q", cfg.Password)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
	if cfg.Interval != 120*time.Second {
		t.Errorf("Interval = %v", cfg.Interval)
	}
}

func TestCollect_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleMetrics))
	}))
	defer srv.Close()

	probe, err := NewClickHouseProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewClickHouseProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect returned no datapoints")
	}

	byName := make(map[string]float64, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.clickhouse.up"] != 1 {
		t.Errorf("senhub.clickhouse.up = %v, want 1", byName["senhub.clickhouse.up"])
	}
	if byName["clickhouse.queries.active"] != 3 {
		t.Errorf("clickhouse.queries.active = %v, want 3", byName["clickhouse.queries.active"])
	}
	if byName["clickhouse.connections"] != 10 {
		t.Errorf("clickhouse.connections = %v, want 10", byName["clickhouse.connections"])
	}
	if byName["clickhouse.parts.active"] != 42 {
		t.Errorf("clickhouse.parts.active = %v, want 42", byName["clickhouse.parts.active"])
	}
	if byName["clickhouse.merges.active"] != 2 {
		t.Errorf("clickhouse.merges.active = %v, want 2", byName["clickhouse.merges.active"])
	}
	if byName["clickhouse.uptime"] != 3600 {
		t.Errorf("clickhouse.uptime = %v, want 3600", byName["clickhouse.uptime"])
	}
}

func TestCollect_Down(t *testing.T) {
	probe, err := NewClickHouseProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19999", // nothing listening
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewClickHouseProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error on scrape failure: %v", err)
	}

	byName := make(map[string]float64)
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.clickhouse.up"] != 0 {
		t.Errorf("senhub.clickhouse.up = %v, want 0 when server unreachable", byName["senhub.clickhouse.up"])
	}
	// No other metrics emitted when unreachable.
	if len(byName) != 1 {
		t.Errorf("expected only senhub.clickhouse.up when server is down, got %d metrics: %v", len(byName), byName)
	}
}

func TestCollect_BasicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()

	probe, err := NewClickHouseProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "ops",
		"password": "hunter2",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewClickHouseProbe: %v", err)
	}

	_, _ = probe.Collect()

	if gotUser != "ops" || gotPass != "hunter2" {
		t.Errorf("Basic Auth not forwarded: user=%q pass=%q", gotUser, gotPass)
	}
}

func TestCollect_EnrichesWithProbeName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(sampleMetrics))
	}))
	defer srv.Close()

	p, err := NewClickHouseProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewClickHouseProbe: %v", err)
	}
	// Give the probe a name so EnrichDataPointsWithProbeName can tag it.
	chProbe := p.(*ClickHouseProbe)
	chProbe.BaseProbe.SetName("my-clickhouse")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "probe_type" && tag.Value != ProbeType {
				t.Errorf("probe_type tag = %q, want %q", tag.Value, ProbeType)
			}
		}
	}
}
