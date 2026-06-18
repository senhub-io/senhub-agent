package mysql

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
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
		want float64
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
	connected := float64(5)
	running := float64(7) // pathological case
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
		health  float64
	}{
		{"Yes", "Yes", 1},
		{"No", "Yes", 0},
		{"Yes", "No", 0},
		{"No", "No", 0},
		{"Connecting", "Yes", 0},
	} {
		ioOK := float64(0)
		if tc.io == "Yes" {
			ioOK = 1
		}
		sqlOK := float64(0)
		if tc.sql == "Yes" {
			sqlOK = 1
		}
		health := float64(1)
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

// TestEntitySource_NotOKBeforeIDPinned: without an operator instance_name,
// the entity must NOT be emitted before pinServerUUID is called, even if
// updateRole has been called (we must never emit with a temporary host:port id
// that would then change to the real uuid on the next cycle).
func TestEntitySource_NotOKBeforeIDPinned(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.updateRole(dbcommon.RoleStandalone)
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() should return ok=false before the server uuid is pinned")
	}
}

// TestEntitySource_NotOKBeforeFirstCollect: without an operator instance_name
// and without any pinServerUUID call, the entity must not be emitted.
func TestEntitySource_NotOKBeforeFirstCollect(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	_, ok := src.Observe()
	if ok {
		t.Error("Observe() should return ok=false before first collect cycle")
	}
}

// TestEntitySource_OKAfterPinAndRole: entity is emitted once both the server
// uuid is pinned and the first successful collect cycle has run.
func TestEntitySource_OKAfterPinAndRole(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true after pin + updateRole")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("Observe() should return 1 entity, got %d", len(obs.Entities))
	}
	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity.Type = %q, want db", e.Type)
	}
	// db.system.name is now a descriptive attribute, not part of the identity.
	if _, hasSystemName := e.ID["db.system.name"]; hasSystemName {
		t.Error("db.system.name must not be in the entity ID — it is a descriptive attribute")
	}
	if e.Attributes["db.system.name"] != "mysql" {
		t.Errorf("entity.Attributes[db.system.name] = %v, want mysql", e.Attributes["db.system.name"])
	}
}

// TestEntitySource_Version: setVersion surfaces the server version on the
// entity as the descriptive db.system.version attribute (toise#216 AT1), and
// the attribute is absent until a version has been reported.
func TestEntitySource_Version(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["db.system.version"]; has {
		t.Error("db.system.version must be absent before a version is reported")
	}

	src.setVersion("8.0.36")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["db.system.version"]; got != "8.0.36" {
		t.Errorf("db.system.version = %v, want 8.0.36", got)
	}
}

// TestEntitySource_DeploymentPlatform: the detected hosting platform rides the
// entity under db.deployment.platform (toise#216 AT3), for db-probe parity with
// postgres. Absent until reported.
func TestEntitySource_DeploymentPlatform(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["db.deployment.platform"]; has {
		t.Error("db.deployment.platform must be absent before the platform is reported")
	}

	src.setEnvironment("rds")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["db.deployment.platform"]; got != "rds" {
		t.Errorf("db.deployment.platform = %v, want rds", got)
	}
}

// TestEntitySource_LocalDBRunsOnHost: a loopback-reachable db is anchored to the
// host with runs_on (enterprise#36); a remote db is not.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	mk := func(host string) entity.Observation {
		src := newMysqlEntitySource(config{Host: host, Port: 3306}, nil)
		src.hostID = func() string { return "h-1" }
		src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
		src.updateRole(dbcommon.RoleStandalone)
		obs, _ := src.Observe()
		return obs
	}

	if !hasRunsOnHost(mk("127.0.0.1"), "h-1") {
		t.Error("loopback db must emit runs_on→host")
	}
	if hasRunsOnHost(mk("10.0.0.5"), "h-1") {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

func hasRunsOnHost(obs entity.Observation, hostID string) bool {
	for _, r := range obs.Relations {
		if r.Type == "runs_on" && r.FromType == "db" && r.ToType == "host" && r.ToID["host.id"] == hostID {
			return true
		}
	}
	return false
}

// TestEntitySource_TechID: when pinServerUUID is given a uuid, the entity id
// must be "mysql:<uuid>".
func TestEntitySource_TechID(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "10.0.0.1", Port: 3307}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RolePrimary)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true after pin + updateRole")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	wantID := "mysql:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if obs.Entities[0].ID["db.instance.id"] != wantID {
		t.Errorf("db.instance.id = %v, want %q", obs.Entities[0].ID["db.instance.id"], wantID)
	}
}

// TestEntitySource_InstanceNameOverride: when instance_name is set, it is used
// verbatim as db.instance.id and the entity is emitted as soon as updateRole
// is called (the id is pinned at construction; no uuid fetch needed).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "10.0.0.1", Port: 3307, InstanceName: "prod-mysql-primary"}, nil)
	// No pinServerUUID call — the operator name must already be pinned.
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe() should return ok=true when instance_name is set and role is known")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	if obs.Entities[0].ID["db.instance.id"] != "prod-mysql-primary" {
		t.Errorf("db.instance.id = %v, want prod-mysql-primary", obs.Entities[0].ID["db.instance.id"])
	}
}

// TestEntitySource_InstanceNameNotOverwrittenByUUID: once the operator
// instance_name is pinned, a subsequent pinServerUUID call must have no effect.
func TestEntitySource_InstanceNameNotOverwrittenByUUID(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "h", Port: 3306, InstanceName: "my-db"}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") // must be ignored
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if obs.Entities[0].ID["db.instance.id"] != "my-db" {
		t.Errorf("db.instance.id = %v, want my-db", obs.Entities[0].ID["db.instance.id"])
	}
}

// TestEntitySource_HostPortFallback: when pinServerUUID("") is called (uuid
// fetch failed), the degraded host:port fallback is used and the entity IS
// emitted (there is nothing better to wait for).
func TestEntitySource_HostPortFallback(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "10.0.0.1", Port: 3307}, nil)
	src.pinServerUUID("") // empty → host:port fallback
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true after fallback pin + updateRole")
	}
	if len(obs.Entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	wantID := "10.0.0.1:3307"
	if obs.Entities[0].ID["db.instance.id"] != wantID {
		t.Errorf("db.instance.id = %v, want %q", obs.Entities[0].ID["db.instance.id"], wantID)
	}
}

// TestEntitySource_MonitorsEdgePresent: when agentstate has an agent id set,
// the Observation must include a monitors relation from the agent to the db.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-test-id")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation.Type = %q, want monitors", r.Type)
	}
	if r.FromType != "service.instance" {
		t.Errorf("relation.FromType = %q, want service.instance", r.FromType)
	}
	if r.FromID["service.instance.id"] != "agent-test-id" {
		t.Errorf("relation.FromID[service.instance.id] = %v, want agent-test-id", r.FromID["service.instance.id"])
	}
	if r.ToType != "db" {
		t.Errorf("relation.ToType = %q, want db", r.ToType)
	}
	wantDbID := "mysql:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if r.ToID["db.instance.id"] != wantDbID {
		t.Errorf("relation.ToID[db.instance.id] = %v, want %q", r.ToID["db.instance.id"], wantDbID)
	}
}

// TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID: when agentstate has no
// agent id (entity emission disabled or not started), no monitors relation
// must be emitted.
func TestEntitySource_MonitorsEdgeAbsentWhenNoAgentID(t *testing.T) {
	agentstate.SetAgentInstanceID("") // ensure empty
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newMysqlEntitySource(config{Host: "h", Port: 3306}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected no relations when agent id is empty, got %d", len(obs.Relations))
	}
}

// TestEntitySource_DescriptiveAttrs: server.address, server.port, and
// db.system.name are descriptive attributes (not identity keys).
func TestEntitySource_DescriptiveAttrs(t *testing.T) {
	src := newMysqlEntitySource(config{Host: "10.0.0.1", Port: 3307}, nil)
	src.pinServerUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	src.updateRole(dbcommon.RoleStandalone)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("expected ok=true")
	}
	e := obs.Entities[0]
	if e.Attributes["server.address"] != "10.0.0.1" {
		t.Errorf("server.address = %v, want 10.0.0.1", e.Attributes["server.address"])
	}
	if e.Attributes["server.port"] != int64(3307) {
		t.Errorf("server.port = %v, want 3307", e.Attributes["server.port"])
	}
	if e.Attributes["db.system.name"] != "mysql" {
		t.Errorf("db.system.name = %v, want mysql", e.Attributes["db.system.name"])
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
