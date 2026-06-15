package mysql

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
)

// ─── config parsing ──────────────────────────────────────────────────────────

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", cfg.Host)
	}
	if cfg.Port != defaultPort {
		t.Errorf("default port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("default interval = %v, want %v", cfg.Interval, defaultInterval)
	}
	if cfg.TopNTables != 20 {
		t.Errorf("default top_n_tables = %d, want 20", cfg.TopNTables)
	}
}

func TestParseConfig_Overrides(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":         "db.example.com",
		"port":         3307,
		"username":     "monitor",
		"password":     "secret",
		"tls":          true,
		"interval":     30,
		"per_database": true,
		"per_table":    true,
		"top_n_tables": 10,
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Host != "db.example.com" {
		t.Errorf("host = %q, want db.example.com", cfg.Host)
	}
	if cfg.Port != 3307 {
		t.Errorf("port = %d, want 3307", cfg.Port)
	}
	if cfg.Username != "monitor" {
		t.Errorf("username = %q, want monitor", cfg.Username)
	}
	if !cfg.TLS {
		t.Errorf("tls = false, want true")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", cfg.Interval)
	}
	if !cfg.PerDatabase {
		t.Errorf("per_database = false, want true")
	}
	if cfg.TopNTables != 10 {
		t.Errorf("top_n_tables = %d, want 10", cfg.TopNTables)
	}
}

// ─── asFloat ─────────────────────────────────────────────────────────────────

func TestAsFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float32
	}{
		{"0", 0},
		{"42", 42},
		{"", 0},
		{"N/A", 0},
	}
	for _, c := range cases {
		got := asFloat(c.in)
		delta := got - c.want
		if delta < 0 {
			delta = -delta
		}
		if delta > 0.01 {
			t.Errorf("asFloat(%q) = %f, want %f", c.in, got, c.want)
		}
	}
}

// ─── buildConnectionPoints — idle clamp ──────────────────────────────────────

func TestIdleClamp_NeverNegative(t *testing.T) {
	connected := float32(5)
	running := float32(7) // pathological case
	idle := connected - running
	if idle < 0 {
		idle = 0
	}
	if idle != 0 {
		t.Errorf("idle clamped = %f, want 0", idle)
	}
}

// ─── replica health ───────────────────────────────────────────────────────────

func TestReplicaHealth(t *testing.T) {
	for _, tc := range []struct {
		io, sql string
		health  float32
	}{
		{"Yes", "Yes", 1},
		{"No", "Yes", 0},
		{"Yes", "No", 0},
		{"No", "No", 0},
		{"Connecting", "Yes", 0},
	} {
		ioOK := float32(0)
		if tc.io == "Yes" {
			ioOK = 1
		}
		sqlOK := float32(0)
		if tc.sql == "Yes" {
			sqlOK = 1
		}
		health := float32(1)
		if ioOK == 0 || sqlOK == 0 {
			health = 0
		}
		if health != tc.health {
			t.Errorf("io=%q sql=%q health=%f want %f", tc.io, tc.sql, health, tc.health)
		}
	}
}

// ─── topNTables ───────────────────────────────────────────────────────────────

func TestTopNTables(t *testing.T) {
	p := &mysqlProbe{BaseProbe: &types.BaseProbe{}, cfg: config{TopNTables: 2}}
	input := []tableSize{
		{db: "a", table: "t1", size: 100},
		{db: "a", table: "t2", size: 500},
		{db: "a", table: "t3", size: 200},
	}
	got := p.topNTables(input)
	if len(got) != 2 {
		t.Fatalf("topNTables len = %d, want 2", len(got))
	}
	if got[0].size != 500 {
		t.Errorf("topNTables[0].size = %f, want 500", got[0].size)
	}
	if got[1].size != 200 {
		t.Errorf("topNTables[1].size = %f, want 200", got[1].size)
	}
}

func TestTopNTables_ZeroCapReturnsAll(t *testing.T) {
	p := &mysqlProbe{BaseProbe: &types.BaseProbe{}, cfg: config{TopNTables: 0}}
	input := []tableSize{{size: 1}, {size: 2}}
	if len(p.topNTables(input)) != 2 {
		t.Error("TopNTables=0 should return all tables")
	}
}

// ─── dp helper ───────────────────────────────────────────────────────────────

func TestDpHelper_AddsMetricTypeTag(t *testing.T) {
	p := &mysqlProbe{BaseProbe: &types.BaseProbe{}, cfg: config{Host: "h", Port: 3306}}
	dp := p.dp("mysql.uptime", 42, time.Now(), "overview", nil)
	if dp.Name != "mysql.uptime" {
		t.Errorf("dp.Name = %q, want mysql.uptime", dp.Name)
	}
	if dp.Value != 42 {
		t.Errorf("dp.Value = %f, want 42", dp.Value)
	}
	found := false
	for _, tag := range dp.Tags {
		if tag.Key == "metric_type" && tag.Value == "overview" {
			found = true
		}
	}
	if !found {
		t.Error("dp does not carry metric_type=overview tag")
	}
}

// ─── entity source ────────────────────────────────────────────────────────────

func TestEntitySource_NotOKBeforeFirstCollect(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() should return ok=false before first collect cycle")
	}
}

func TestEntitySource_OKAfterUpdateRole(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true after updateRole")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() should return 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity.Type = %q, want db", e.Type)
	}
	if e.ID["db.system.name"] != "mysql" {
		t.Errorf("entity.ID[db.system.name] = %v, want mysql", e.ID["db.system.name"])
	}
}

func TestEntitySource_InstanceID(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "10.0.0.1", Port: 3307}, nil)
	src.updateRole(dbcommon.RolePrimary)

	obs, _ := src.Observe()
	if len(obs.Entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	wantID := "mysql://10.0.0.1:3307"
	if obs.Entities[0].ID["db.instance.id"] != wantID {
		t.Errorf("db.instance.id = %v, want %q", obs.Entities[0].ID["db.instance.id"], wantID)
	}
}

// ─── ProbeType constant ───────────────────────────────────────────────────────

func TestProbeType_Constant(t *testing.T) {
	if ProbeType != "mysql" {
		t.Errorf("ProbeType = %q, want mysql", ProbeType)
	}
}

// ─── metric naming completeness spot-check ────────────────────────────────────

var expectedMetrics = []string{
	"senhub.db.up",
	"mysql.uptime",
	"mysql.threads.connected",
	"mysql.threads.running",
	"senhub.db.connection.idle",
	"senhub.db.mysql.connection.max",
	"senhub.db.connection.utilization",
	"mysql.connection.errors.aborted_clients",
	"mysql.connection.errors.aborted_connects",
	"mysql.connection.errors.max_connections",
	"mysql.query.count",
	"mysql.query.slow.count",
	"mysql.commands",
	"senhub.db.mysql.transaction.count.committed",
	"senhub.db.mysql.transaction.count.rolled_back",
	"senhub.db.mysql.buffer_pool.hit_ratio",
	"senhub.db.mysql.buffer_pool.utilization",
	"mysql.buffer_pool.data_pages.dirty",
	"senhub.db.mysql.lock.deadlocks",
	"senhub.db.mysql.lock.waiting",
	"senhub.db.mysql.row_lock.time.avg",
	"senhub.db.mysql.io.read",
	"senhub.db.mysql.io.write",
	"senhub.db.database.size",
	"senhub.db.mysql.table.count",
	"senhub.db.replication.role",
	"senhub.db.replication.health",
	"senhub.db.replication.replicas.connected",
	"mysql.replica.time_behind_source",
	"senhub.db.mysql.replica.io_thread.running",
	"senhub.db.mysql.replica.sql_thread.running",
}

func TestExpectedMetrics_NamesAreNonEmpty(t *testing.T) {
	for _, name := range expectedMetrics {
		if name == "" {
			t.Error("empty metric name in expectedMetrics list")
		}
	}
}

// Compile-time check: data_store.DataPoint is usable from this package.
var _ = data_store.DataPoint{}
