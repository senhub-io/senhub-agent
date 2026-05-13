package mysql

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestParseConfig_MinimumRequired(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":     "db.example.com",
		"username": "monitor",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "db.example.com" {
		t.Errorf("Host = %q, want db.example.com", cfg.Host)
	}
	if cfg.Port != 3306 {
		t.Errorf("Port default = %d, want 3306", cfg.Port)
	}
	if cfg.Interval != 60 {
		t.Errorf("Interval default = %d, want 60", cfg.Interval)
	}
	if cfg.Timeout != 10 {
		t.Errorf("Timeout default = %d, want 10", cfg.Timeout)
	}
	if cfg.MaxReplicationLagSeconds != 60 {
		t.Errorf("MaxReplicationLagSeconds default = %d, want 60", cfg.MaxReplicationLagSeconds)
	}
	if cfg.MaxHeartbeatAgeSeconds != 300 {
		t.Errorf("MaxHeartbeatAgeSeconds default = %d, want 300", cfg.MaxHeartbeatAgeSeconds)
	}
	if cfg.ExposePerDatabase {
		t.Errorf("ExposePerDatabase should default to false")
	}
	if cfg.ExposeTopTables != 0 {
		t.Errorf("ExposeTopTables default = %d, want 0", cfg.ExposeTopTables)
	}
}

func TestParseConfig_RejectsMissingHost(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"username": "monitor",
	})
	if err == nil {
		t.Errorf("missing host should error")
	}
}

func TestParseConfig_RejectsMissingUsername(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host": "db.example.com",
	})
	if err == nil {
		t.Errorf("missing username should error")
	}
}

func TestParseConfig_TLSBlock(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":     "db.example.com",
		"username": "monitor",
		"password": "secret",
		"tls": map[string]interface{}{
			"enabled":     true,
			"skip_verify": true,
			"ca_file":     "/etc/ssl/db-ca.pem",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.TLSEnabled || !cfg.TLSSkipVerify || cfg.TLSCAFile != "/etc/ssl/db-ca.pem" {
		t.Errorf("TLS block not fully parsed: %+v", cfg)
	}
}

func TestParseConfig_OptInFlags(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":                     "db.example.com",
		"username":                 "monitor",
		"password":                 "secret",
		"expose_per_database":      true,
		"include_system_databases": true,
		"expose_top_tables":        25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ExposePerDatabase {
		t.Errorf("ExposePerDatabase not picked up")
	}
	if !cfg.IncludeSystemDatabases {
		t.Errorf("IncludeSystemDatabases not picked up")
	}
	if cfg.ExposeTopTables != 25 {
		t.Errorf("ExposeTopTables = %d, want 25", cfg.ExposeTopTables)
	}
}

func TestBuildDSN_TLSDisabled(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Database: "production", Timeout: 10,
	})
	// The exact DSN format is driver-specific, but a few invariants
	// must hold or the connection will fail at runtime.
	want := []string{
		"monitor:secret",
		"@tcp(db.example.com:3306)",
		"/production",
		"timeout=10s",
		"tls=false",
		"parseTime=true",
	}
	for _, w := range want {
		if !strings.Contains(dsn, w) {
			t.Errorf("DSN %q missing %q", dsn, w)
		}
	}
}

func TestBuildDSN_TLSSkipVerify(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Timeout: 10, TLSEnabled: true, TLSSkipVerify: true,
	})
	if !strings.Contains(dsn, "tls=skip-verify") {
		t.Errorf("DSN should set tls=skip-verify, got %q", dsn)
	}
}

func TestBuildDSN_TLSEnabledVerify(t *testing.T) {
	dsn := buildDSN(&probeConfig{
		Host: "db.example.com", Port: 3306,
		Username: "monitor", Password: "secret",
		Timeout: 10, TLSEnabled: true, TLSSkipVerify: false,
	})
	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN should set tls=true, got %q", dsn)
	}
}

// TestBuildOverviewMetrics_FromStubMaps drives the family builders
// with synthetic SHOW GLOBAL STATUS / VARIABLES maps so the
// metric-shape contract can be pinned without a real MySQL server.
// Integration with a live MySQL via testcontainers is a separate
// test target (make test-database).
func TestBuildOverviewMetrics_FromStubMaps(t *testing.T) {
	p := stubProbe()
	now := mustParseTime(t, "2026-05-13T11:00:00Z")
	status := map[string]string{"Uptime": "12345", "Threads_connected": "42"}
	vars := map[string]string{"max_connections": "200"}

	points := p.buildOverviewMetrics(now, status, vars, dbcommon.RoleStandalone)

	got := nameSet(points)
	for _, expect := range []string{
		"db_uptime_seconds", "db_version_info",
		"db_connections_utilization", "db_replication_role",
	} {
		if !got[expect] {
			t.Errorf("overview missing %q (got %v)", expect, keys(got))
		}
	}
}

func TestBuildConnectionsMetrics_FromStubMaps(t *testing.T) {
	p := stubProbe()
	now := mustParseTime(t, "2026-05-13T11:00:00Z")
	status := map[string]string{
		"Threads_running":                   "5",
		"Threads_connected":                 "20",
		"Aborted_clients":                   "3",
		"Aborted_connects":                  "1",
		"Connection_errors_max_connections": "0",
	}
	vars := map[string]string{"max_connections": "100"}

	points := p.buildConnectionsMetrics(now, status, vars)

	want := map[string]float32{
		"db_connections_active":   5,
		"db_connections_idle":     15,
		"db_connections_max":      100,
		"db_connections_aborted":  4,
		"db_connections_refused":  0,
	}
	gotByName := valueByName(points)
	for n, expected := range want {
		if v, ok := gotByName[n]; !ok || v != expected {
			t.Errorf("%s = %v (present=%v), want %v", n, v, ok, expected)
		}
	}
}

func TestBuildThroughputMetrics_PerCommandTag(t *testing.T) {
	p := stubProbe()
	now := mustParseTime(t, "2026-05-13T11:00:00Z")
	status := map[string]string{
		"Questions":               "100000",
		"Com_commit":              "50000",
		"Com_rollback":            "100",
		"Com_select":              "80000",
		"Com_insert":              "10000",
		"Com_update":              "5000",
		"Com_delete":              "5000",
		"Com_replace":             "0",
		"Slow_queries":            "12",
		"Created_tmp_disk_tables": "2",
		"Created_tmp_tables":      "98",
	}
	points := p.buildThroughputMetrics(now, status)

	// Per-verb metric: same metric name across verbs, distinguished
	// by the `command` tag. Five verbs → five points.
	verbCount := 0
	for _, pt := range points {
		if pt.Name == "db_mysql_command_count" {
			verbCount++
			var verb string
			for _, t := range pt.Tags {
				if t.Key == "command" {
					verb = t.Value
				}
			}
			if verb == "" {
				t.Errorf("db_mysql_command_count without command tag")
			}
		}
	}
	if verbCount != 5 {
		t.Errorf("expected 5 per-verb points, got %d", verbCount)
	}
}

func TestBuildReplicationMetrics_StandaloneEmitsNothing(t *testing.T) {
	p := stubProbe()
	now := mustParseTime(t, "2026-05-13T11:00:00Z")
	points, health := p.buildReplicationMetrics(nil, now, map[string]string{}, dbcommon.RoleStandalone)
	if len(points) != 0 {
		t.Errorf("standalone role should emit no replication points, got %d", len(points))
	}
	if health != 1 {
		t.Errorf("standalone health = %v, want 1 (no replication problem to detect)", health)
	}
}

func TestBuildCacheMetrics_HitRatioClamp(t *testing.T) {
	p := stubProbe()
	now := mustParseTime(t, "2026-05-13T11:00:00Z")
	status := map[string]string{
		"Innodb_buffer_pool_reads":           "10",
		"Innodb_buffer_pool_read_requests":   "1000",
		"Innodb_buffer_pool_pages_data":      "9000",
		"Innodb_buffer_pool_pages_total":     "10000",
		"Innodb_buffer_pool_pages_dirty":     "150",
	}
	points := p.buildCacheMetrics(now, status)
	got := valueByName(points)
	// Hit ratio = (1000-10)/1000 = 0.99
	if v := got["db_buffer_hit_ratio"]; v < 0.989 || v > 0.991 {
		t.Errorf("buffer hit ratio = %v, want ~0.99", v)
	}
	// Utilization = 9000/10000 = 0.9
	if v := got["db_buffer_utilization"]; v < 0.89 || v > 0.91 {
		t.Errorf("buffer utilization = %v, want ~0.9", v)
	}
	if v := got["db_buffer_dirty_pages"]; v != 150 {
		t.Errorf("dirty pages = %v, want 150", v)
	}
}

// --- helpers ---

func stubProbe() *mysqlProbe {
	return &mysqlProbe{
		cfg:           &probeConfig{Host: "test", Port: 3306},
		versionString: "8.0.32-MockServer",
		environment:   "self_hosted",
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func nameSet(points []datapoint.DataPoint) map[string]bool {
	out := make(map[string]bool, len(points))
	for _, p := range points {
		out[p.Name] = true
	}
	return out
}

func valueByName(points []datapoint.DataPoint) map[string]float32 {
	out := make(map[string]float32, len(points))
	for _, p := range points {
		out[p.Name] = p.Value
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBuildPerDatabaseMetrics_OptInGate confirms that the
// per-database family is silently skipped when the operator hasn't
// flipped the expose_per_database flag. The Prometheus cardinality
// contract (per DESIGN §6) hinges on this gate.
func TestBuildPerDatabaseMetrics_OptInGate(t *testing.T) {
	p := stubProbe()
	// expose_per_database left at default false
	points := p.buildPerDatabaseMetrics(nil, time.Now())
	if len(points) != 0 {
		t.Errorf("opt-in OFF should emit nothing, got %d points", len(points))
	}
}

func TestBuildPerTableMetrics_OptInGate(t *testing.T) {
	p := stubProbe()
	// expose_top_tables left at default 0
	points := p.buildPerTableMetrics(nil, time.Now())
	if len(points) != 0 {
		t.Errorf("opt-in OFF (expose_top_tables=0) should emit nothing, got %d", len(points))
	}
}
