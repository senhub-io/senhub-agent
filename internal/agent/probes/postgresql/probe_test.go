package postgresql

import (
	"net/url"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// TestParseConfig_Defaults exercises the config parser with the minimum
// required fields and verifies that defaults are applied.
func TestParseConfig_Defaults(t *testing.T) {
	params := map[string]interface{}{
		"host":     "pg.example.com",
		"username": "mon",
		"password": "secret",
	}

	cfg, err := parseConfig(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Host != "pg.example.com" {
		t.Errorf("host: got %q, want %q", cfg.Host, "pg.example.com")
	}
	if cfg.Port != 5432 {
		t.Errorf("port: got %d, want 5432", cfg.Port)
	}
	if cfg.Username != "mon" {
		t.Errorf("username: got %q, want %q", cfg.Username, "mon")
	}
	if cfg.Password != "secret" {
		t.Errorf("password: got %q, want %q", cfg.Password, "secret")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("interval: got %v, want 60s", cfg.Interval)
	}
	if cfg.TLSConfig != nil {
		t.Errorf("TLSConfig: expected nil by default, got %v", cfg.TLSConfig)
	}
}

// TestParseConfig_AllFields exercises non-default values.
func TestParseConfig_AllFields(t *testing.T) {
	params := map[string]interface{}{
		"host":      "10.0.0.1",
		"port":      5433,
		"username":  "admin",
		"password":  "pass123",
		"databases": []interface{}{"app", "reporting"},
		"interval":  30,
		"tls": map[string]interface{}{
			"insecure_skip_verify": true,
		},
	}

	cfg, err := parseConfig(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 5433 {
		t.Errorf("port: got %d, want 5433", cfg.Port)
	}
	if len(cfg.Databases) != 2 {
		t.Errorf("databases: got %d, want 2", len(cfg.Databases))
	}
	if cfg.Databases[0] != "app" {
		t.Errorf("databases[0]: got %q, want %q", cfg.Databases[0], "app")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval: got %v, want 30s", cfg.Interval)
	}
	if cfg.TLSConfig == nil {
		t.Fatal("TLSConfig: expected non-nil")
	}
	if !cfg.TLSConfig.InsecureSkipVerify {
		t.Error("TLSConfig.InsecureSkipVerify: expected true")
	}
}

// TestParseConfig_MissingHost verifies that a missing host returns an error.
func TestParseConfig_MissingHost(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"username": "mon",
		"password": "pass",
	})
	if err == nil {
		t.Fatal("expected error for missing host, got nil")
	}
}

// TestParseConfig_MissingUsername verifies that a missing username returns an error.
func TestParseConfig_MissingUsername(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host":     "db.local",
		"password": "pass",
	})
	if err == nil {
		t.Fatal("expected error for missing username, got nil")
	}
}

// TestParseConfig_MissingPassword verifies that a missing password returns an error.
func TestParseConfig_MissingPassword(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"host":     "db.local",
		"username": "mon",
	})
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
}

// TestBuildDSN_NoTLS verifies the URL DSN and the opportunistic default
// when TLS is not explicitly configured (sslmode=prefer, not disable).
func TestBuildDSN_NoTLS(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:     "pg.local",
			Port:     5432,
			Username: "mon",
			Password: "pw",
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/postgres?sslmode=prefer"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_WithDatabase verifies that the first configured database
// is used in the DSN path.
func TestBuildDSN_WithDatabase(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:      "pg.local",
			Port:      5432,
			Username:  "mon",
			Password:  "pw",
			Databases: []string{"mydb"},
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/mydb?sslmode=prefer"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_TLSInsecure verifies sslmode=require when insecure_skip_verify.
func TestBuildDSN_TLSInsecure(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:      "pg.local",
			Port:      5432,
			Username:  "mon",
			Password:  "pw",
			TLSConfig: &pgTLSConfig{InsecureSkipVerify: true},
		},
	}

	dsn := p.buildDSN()
	want := "postgres://mon:pw@pg.local:5432/postgres?sslmode=require"
	if dsn != want {
		t.Errorf("DSN mismatch:\n got  %q\n want %q", dsn, want)
	}
}

// TestBuildDSN_NoParameterSmuggling is the regression test for the keyword/
// value injection the space-joined form allowed: a password carrying
// " host=evil.com sslmode=disable" must stay inside the password component
// and never redirect the connection or downgrade TLS.
func TestBuildDSN_NoParameterSmuggling(t *testing.T) {
	p := &pgProbe{
		cfg: config{
			Host:     "pg.local",
			Port:     5432,
			Username: "mon",
			Password: "pw host=evil.com sslmode=disable",
		},
	}

	dsn := p.buildDSN()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("buildDSN produced an unparseable URL %q: %v", dsn, err)
	}
	if u.Hostname() != "pg.local" {
		t.Errorf("host redirected to %q via the password, want pg.local", u.Hostname())
	}
	if got := u.Query().Get("sslmode"); got != "prefer" {
		t.Errorf("sslmode smuggled to %q, want prefer", got)
	}
	pw, _ := u.User.Password()
	if pw != p.cfg.Password {
		t.Errorf("password round-trip = %q, want %q", pw, p.cfg.Password)
	}
}

// TestEntitySource_NotReadyBeforeIDPinned verifies ok=false when no instance_name
// is set and the tech id has not been pinned yet.
func TestEntitySource_NotReadyBeforeIDPinned(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)

	_, ok := src.Observe()
	if ok {
		t.Error("Observe should return ok=false before the tech id is pinned")
	}
}

// TestEntitySource_TechIDPinned verifies that pinTechID + update produces a
// stable "postgresql:<id>" identity and that the entity is ready afterwards.
func TestEntitySource_TechIDPinned(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)

	// Before pinning, update with no fallback must stay not-ready.
	src.update("")
	if _, ok := src.Observe(); ok {
		t.Fatal("Observe should return ok=false before tech id is pinned")
	}

	// Pin the tech id.
	pinned := src.pinTechID("7226200459612946432")
	if !pinned {
		t.Fatal("pinTechID should return true on the first call")
	}
	// Second call is a no-op.
	if src.pinTechID("other") {
		t.Error("pinTechID should return false on subsequent calls")
	}

	src.update("")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe should return ok=true after tech id is pinned and update called")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}

	e := obs.Entities[0]
	if e.Type != "db" {
		t.Errorf("entity type: got %q, want db", e.Type)
	}
	wantID := "postgresql:7226200459612946432"
	if got := e.ID["db.instance.id"]; got != wantID {
		t.Errorf("db.instance.id: got %v, want %s", got, wantID)
	}
	// Identity is single-key {db.instance.id}: db.system.name is a descriptive
	// attribute, NOT an identity key. A second identity key would make the
	// monitors ToID (which carries only db.instance.id) unresolvable, orphaning
	// the db in the consumer graph (#504).
	if len(e.ID) != 1 {
		t.Errorf("identity must be single-key {db.instance.id}, got %v", e.ID)
	}
	if got := e.Attributes["db.system.name"]; got != "postgresql" {
		t.Errorf("db.system.name must be a descriptive attribute: got %v, want postgresql", got)
	}
	// server.address/port must be descriptive attrs, not part of the id.
	if _, inID := e.ID["server.address"]; inID {
		t.Error("server.address must be a descriptive attribute, not an id key")
	}
	if got := e.Attributes["server.address"]; got != "pg.local" {
		t.Errorf("server.address attr: got %v, want pg.local", got)
	}
	if got := e.Attributes["server.port"]; got != int64(5432) {
		t.Errorf("server.port attr: got %v, want 5432", got)
	}
	// The monitors edge ToID must match the entity identity exactly, else Toise
	// drops the edge and the db floats (#504). The edge is built at update()
	// time, so refresh the cache after the agent id becomes available (the probe
	// calls update() every Collect cycle).
	agentstate.SetAgentInstanceID("agent-key")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })
	src.update("")
	obs2, _ := src.Observe()
	var found bool
	for _, r := range obs2.Relations {
		if r.Type != "monitors" {
			continue
		}
		found = true
		if r.ToID["db.instance.id"] != wantID || len(r.ToID) != 1 {
			t.Errorf("monitors ToID must equal the single-key identity %q, got %v", wantID, r.ToID)
		}
	}
	if !found {
		t.Errorf("expected a monitors edge once the agent id is set, got %+v", obs2.Relations)
	}
}

// TestEntitySource_Version verifies setVersion surfaces the parsed short
// version on the entity as db.system.version (toise#216 AT1), absent until set.
func TestEntitySource_Version(t *testing.T) {
	src := newPgEntitySource(config{Host: "pg.local", Port: 5432, InstanceName: "p"}, nil)

	src.update("")
	obs, _ := src.Observe()
	if _, has := obs.Entities[0].Attributes["db.system.version"]; has {
		t.Error("db.system.version must be absent before a version is reported")
	}

	src.setVersion("16.1")
	src.update("")
	obs, _ = src.Observe()
	if got := obs.Entities[0].Attributes["db.system.version"]; got != "16.1" {
		t.Errorf("db.system.version = %v, want 16.1", got)
	}
}

// TestEntitySource_ReplicationRoleKey verifies the replication role rides under
// the canonical replication.role key (matching mysql), not the legacy bare role
// (toise#216 AT2).
func TestEntitySource_ReplicationRoleKey(t *testing.T) {
	src := newPgEntitySource(config{Host: "pg.local", Port: 5432, InstanceName: "p"}, nil)
	src.setRole(dbcommon.RoleReplica)
	src.update("")

	obs, _ := src.Observe()
	e := obs.Entities[0]
	if got := e.Attributes["replication.role"]; got != "replica" {
		t.Errorf("replication.role = %v, want replica", got)
	}
	if _, stale := e.Attributes["role"]; stale {
		t.Error("legacy bare role key must no longer be emitted (toise#216 AT2)")
	}
}

// TestEntitySource_DeploymentPlatform verifies the hosting platform rides under
// the canonical db.deployment.platform key, not the legacy `environment` (which
// conflated platform with deployment tier — toise#216 AT3).
func TestEntitySource_DeploymentPlatform(t *testing.T) {
	src := newPgEntitySource(config{Host: "pg.local", Port: 5432, InstanceName: "p"}, nil)
	src.setEnvironment(dbcommon.EnvironmentSelfHosted)
	src.update("")

	obs, _ := src.Observe()
	e := obs.Entities[0]
	if got := e.Attributes["db.deployment.platform"]; got != "self_hosted" {
		t.Errorf("db.deployment.platform = %v, want self_hosted", got)
	}
	if _, stale := e.Attributes["environment"]; stale {
		t.Error("legacy environment key must no longer be emitted (toise#216 AT3)")
	}
}

// TestEntitySource_LocalDBRunsOnHost: a loopback-reachable db is anchored to the
// host with runs_on (enterprise#36); a remote db is not.
func TestEntitySource_LocalDBRunsOnHost(t *testing.T) {
	mk := func(host string) entity.Observation {
		src := newPgEntitySource(config{Host: host, Port: 5432, InstanceName: "p"}, nil)
		src.hostID = func() string { return "h-1" }
		src.update("")
		obs, _ := src.Observe()
		return obs
	}
	runsOn := func(obs entity.Observation) bool {
		for _, r := range obs.Relations {
			if r.Type == "runs_on" && r.FromType == "db" && r.ToID["host.id"] == "h-1" {
				return true
			}
		}
		return false
	}
	if !runsOn(mk("127.0.0.1")) {
		t.Error("loopback db must emit runs_on→host")
	}
	if runsOn(mk("db.example.com")) {
		t.Error("remote db must NOT emit runs_on→host")
	}
}

// TestEntitySource_InstanceNameOverride verifies that an operator-supplied
// instance_name is used verbatim as db.instance.id and is ready after the
// first update (no waiting for tech id fetch).
func TestEntitySource_InstanceNameOverride(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432, InstanceName: "prod-primary"},
		nil,
	)

	// instance_name is already pinned at construction.
	if src.pinnedID() != "prod-primary" {
		t.Fatalf("pinnedID: got %q, want prod-primary", src.pinnedID())
	}

	src.update("")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe should return ok=true after update with instance_name set")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(obs.Entities))
	}
	if got := obs.Entities[0].ID["db.instance.id"]; got != "prod-primary" {
		t.Errorf("db.instance.id: got %v, want prod-primary", got)
	}
}

// TestEntitySource_HostPortFallback verifies the documented db degraded
// fallback: when no instance_name is set and no tech id is available the
// caller may pass a non-empty host:port string to update() so the entity is
// emitted rather than withheld indefinitely.
func TestEntitySource_HostPortFallback(t *testing.T) {
	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)

	// Pass the fallback explicitly (caller gives up on tech id).
	src.update("pg.local:5432")
	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe should return ok=true after host:port fallback update")
	}
	if got := obs.Entities[0].ID["db.instance.id"]; got != "pg.local:5432" {
		t.Errorf("db.instance.id: got %v, want pg.local:5432", got)
	}
}

// TestEntitySource_MonitorsEdgePresent verifies that when agentstate has a
// non-empty agent id, the observation contains a monitors relation.
func TestEntitySource_MonitorsEdgePresent(t *testing.T) {
	agentstate.SetAgentInstanceID("agent-abc")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)
	src.pinTechID("7226200459612946432")
	src.update("")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want ok=true")
	}
	if len(obs.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(obs.Relations))
	}
	r := obs.Relations[0]
	if r.Type != "monitors" {
		t.Errorf("relation type: got %q, want monitors", r.Type)
	}
	if r.FromType != "service.instance" {
		t.Errorf("FromType: got %q, want service.instance", r.FromType)
	}
	if r.FromID["service.instance.id"] != "agent-abc" {
		t.Errorf("FromID: got %v, want agent-abc", r.FromID["service.instance.id"])
	}
	if r.ToType != "db" {
		t.Errorf("ToType: got %q, want db", r.ToType)
	}
	if r.ToID["db.instance.id"] != "postgresql:7226200459612946432" {
		t.Errorf("ToID: got %v, want postgresql:7226200459612946432", r.ToID["db.instance.id"])
	}
}

// TestEntitySource_MonitorsEdgeAbsent verifies that when agentstate has no
// agent id the monitors relation is omitted (not emitted with an empty source).
func TestEntitySource_MonitorsEdgeAbsent(t *testing.T) {
	agentstate.SetAgentInstanceID("")
	t.Cleanup(func() { agentstate.SetAgentInstanceID("") })

	src := newPgEntitySource(
		config{Host: "pg.local", Port: 5432},
		nil,
	)
	src.pinTechID("7226200459612946432")
	src.update("")

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false, want ok=true")
	}
	if len(obs.Relations) != 0 {
		t.Errorf("expected 0 relations when agentID is empty, got %d", len(obs.Relations))
	}
}

// TestParseConfig_InstanceName verifies that the optional instance_name field
// is parsed and stored correctly.
func TestParseConfig_InstanceName(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"host":          "pg.local",
		"username":      "mon",
		"password":      "secret",
		"instance_name": "prod-primary",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InstanceName != "prod-primary" {
		t.Errorf("InstanceName: got %q, want prod-primary", cfg.InstanceName)
	}
}

// TestProbeType verifies the stable identifier constant.
func TestProbeType(t *testing.T) {
	if ProbeType != "postgresql" {
		t.Errorf("ProbeType: got %q, want %q", ProbeType, "postgresql")
	}
}
