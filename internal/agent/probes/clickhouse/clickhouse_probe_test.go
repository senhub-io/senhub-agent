package clickhouse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// sampleRows is what the HTTP interface answers to systemQuery on a
// server that has run no INSERT yet: system.events leaves InsertQuery out.
const sampleRows = "metrics\tQuery\t3\n" +
	"metrics\tTCPConnection\t4\n" +
	"metrics\tHTTPConnection\t6\n" +
	"metrics\tMemoryTracking\t524288000\n" +
	"metrics\tPartsActive\t42\n" +
	"metrics\tMerge\t2\n" +
	"events\tQuery\t17\n" +
	"events\tSelectQuery\t17\n" +
	"asynchronous_metrics\tUptime\t3600.5\n"

// chServer answers the system-table query and serverUUID() like the
// ClickHouse HTTP interface, and nothing on /metrics.
func chServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), "serverUUID()"):
			_, _ = w.Write([]byte("5a8e0c74-3144-4eaf-89a6-3006e95cbf04\n"))
		case string(body) == systemQuery:
			_, _ = w.Write([]byte(sampleRows))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

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
	srv := chServer(t)
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

	byName := make(map[string]float64, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	want := map[string]float64{
		"senhub.clickhouse.up":      1,
		"clickhouse.queries.active": 3,
		"clickhouse.connections":    10,
		"clickhouse.memory.used":    524288000,
		"clickhouse.parts.active":   42,
		"clickhouse.merges.active":  2,
		"clickhouse.uptime":         3600.5,
		"clickhouse.queries.total":  17,
		"clickhouse.queries.select": 17,
		"clickhouse.queries.insert": 0,
		"clickhouse.written.data":   0,
	}
	for name, v := range want {
		got, ok := byName[name]
		if !ok {
			t.Errorf("%s missing", name)
			continue
		}
		if got != v {
			t.Errorf("%s = %v, want %v", name, got, v)
		}
	}
}

func TestCollect_RefusalCarriesTheServerReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Code: 194. DB::Exception: default: Authentication failed\n\nmore text"))
	}))
	defer srv.Close()

	p, err := NewClickHouseProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewClickHouseProbe: %v", err)
	}
	_, err = p.(*ClickHouseProbe).fetchSystemRows()
	if err == nil || !strings.Contains(err.Error(), "Authentication failed") {
		t.Fatalf("error = %v, want the server's reason", err)
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
		w.WriteHeader(http.StatusOK)
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
	srv := chServer(t)
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
