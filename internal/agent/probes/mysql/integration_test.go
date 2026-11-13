//go:build integration

package mysql

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type itTarget struct {
	host, user, password string
	port                 int
}

func itTargetFromEnv(t *testing.T) itTarget {
	t.Helper()
	host := os.Getenv("MYSQL_TEST_HOST")
	user := os.Getenv("MYSQL_TEST_USER")
	if host == "" || user == "" {
		t.Skip("no MySQL to test against: set MYSQL_TEST_HOST and MYSQL_TEST_USER")
	}
	port := 3306
	if v := os.Getenv("MYSQL_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("MYSQL_TEST_PORT %q is not a number", v)
		}
		port = p
	}
	return itTarget{host: host, port: port, user: user, password: os.Getenv("MYSQL_TEST_PASSWORD")}
}

func (tg itTarget) params() map[string]interface{} {
	return map[string]interface{}{
		"host":     tg.host,
		"port":     tg.port,
		"username": tg.user,
		"password": tg.password,
	}
}

func itLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func itStartParams(t *testing.T, params map[string]interface{}) *mysqlProbe {
	t.Helper()
	p, err := NewMysqlProbe(params, itLogger())
	if err != nil {
		t.Fatalf("NewMysqlProbe: %v", err)
	}
	mp := p.(*mysqlProbe)
	mp.SetName("integration-mysql")
	if err := mp.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		if err := mp.OnShutdown(nil); err != nil {
			t.Errorf("OnShutdown: %v", err)
		}
	})
	return mp
}

func itStart(t *testing.T, tg itTarget) *mysqlProbe {
	return itStartParams(t, tg.params())
}

func itCollect(t *testing.T, p *mysqlProbe) []data_store.DataPoint {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return points
}

func itNamed(points []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, dp := range points {
		if dp.Name == name {
			out = append(out, dp)
		}
	}
	return out
}

func itTag(dp data_store.DataPoint, key string) string {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

func itOne(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	got := itNamed(points, name)
	if len(got) != 1 {
		t.Fatalf("%s: %d datapoints, want exactly 1", name, len(got))
	}
	return got[0]
}

func itObserveID(t *testing.T, p *mysqlProbe) string {
	t.Helper()
	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatalf("Observe ok=false on successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	idAny, ok := obs.Entities[0].ID["db.instance.id"]
	if !ok {
		t.Fatalf("entity id missing db.instance.id")
	}
	id, _ := idAny.(string)
	if id == "" {
		t.Fatalf("entity id empty")
	}
	return id
}

func TestIntegration_Collect_Healthy(t *testing.T) {
	tg := itTargetFromEnv(t)
	p := itStart(t, tg)
	points := itCollect(t, p)

	if up := itOne(t, points, "senhub.db.up"); up.Value != 1 {
		t.Fatalf("senhub.db.up = %v, want 1: login failed as %s to %s:%d", up.Value, tg.user, tg.host, tg.port)
	}

	if check := itOne(t, points, dbcommon.CheckResultMetric); check.Value != float64(dbcommon.CheckOK) {
		t.Errorf("senhub.probe.check.result = %v, want %v", check.Value, float64(dbcommon.CheckOK))
	}
	if uptime := itOne(t, points, "mysql.uptime"); uptime.Value <= 0 {
		t.Errorf("mysql.uptime = %v, want > 0", uptime.Value)
	}
	if conn := itOne(t, points, "mysql.threads.connected"); itTag(conn, "kind") != "connected" || conn.Value < 1 {
		t.Errorf("mysql.threads.connected = %v with kind %q, want >= 1 and kind=connected", conn.Value, itTag(conn, "kind"))
	}
	if version := itOne(t, points, "senhub.db.version.info"); itTag(version, "version") == "" {
		t.Errorf("senhub.db.version.info emitted with empty version tag")
	}
	if limit := itOne(t, points, "senhub.db.mysql.connection.max"); limit.Value <= 0 {
		t.Errorf("senhub.db.mysql.connection.max = %v, want > 0", limit.Value)
	}
	for _, name := range []string{
		"mysql.query.count",
		"mysql.threads.running",
		"senhub.db.connection.idle",
		"senhub.db.replication.role",
		"senhub.db.replication.health",
		"senhub.db.database.size",
		"senhub.db.mysql.table.count",
	} {
		if len(itNamed(points, name)) == 0 {
			t.Errorf("%s not emitted", name)
		}
	}
	if role := itOne(t, points, "senhub.db.replication.role"); itTag(role, "role") != "standalone" {
		t.Errorf("senhub.db.replication.role tag role = %q, want standalone", itTag(role, "role"))
	}
	if health := itOne(t, points, "senhub.db.replication.health"); health.Value != 1 {
		t.Errorf("senhub.db.replication.health = %v, want 1", health.Value)
	}
}

func TestIntegration_Entity(t *testing.T) {
	tg := itTargetFromEnv(t)
	p := itStart(t, tg)
	points := itCollect(t, p)

	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatalf("Observe ok=false on successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(obs.Entities))
	}
	ent := obs.Entities[0]
	if ent.Type != "db" {
		t.Errorf("entity type = %q, want db", ent.Type)
	}
	idAny, ok := ent.ID["db.instance.id"]
	if !ok {
		t.Fatalf("entity id missing db.instance.id")
	}
	id, _ := idAny.(string)
	if id == "" {
		t.Fatalf("entity id empty")
	}
	if !strings.HasPrefix(id, "mysql:") {
		t.Logf("db.instance.id %q does not start with mysql:, acceptable for MariaDB fallback", id)
	}
	if ent.Attributes["db.system.name"] != "mysql" {
		t.Errorf("db.system.name = %v, want mysql", ent.Attributes["db.system.name"])
	}
	if ent.Attributes["server.address"] != tg.host {
		t.Errorf("server.address = %v, want %s", ent.Attributes["server.address"], tg.host)
	}
	version, _ := ent.Attributes["db.system.version"].(string)
	if version == "" {
		t.Errorf("db.system.version empty")
	}
	if ent.Attributes["replication.role"] != "standalone" {
		t.Errorf("replication.role = %v, want standalone", ent.Attributes["replication.role"])
	}
	for _, dp := range points {
		if got := itTag(dp, "db.instance.id"); got != "" && got != id {
			t.Errorf("%s: db.instance.id tag %q != entity id %q", dp.Name, got, id)
		}
	}
}

func TestIntegration_IdentityStableAcrossCollections(t *testing.T) {
	tg := itTargetFromEnv(t)
	p1 := itStart(t, tg)
	_ = itCollect(t, p1)
	id1 := itObserveID(t, p1)

	_ = itCollect(t, p1)
	id2 := itObserveID(t, p1)
	if id1 != id2 {
		t.Errorf("db.instance.id changed between collections: %q -> %q", id1, id2)
	}

	p2 := itStart(t, tg)
	_ = itCollect(t, p2)
	id3 := itObserveID(t, p2)
	if id1 != id3 {
		t.Errorf("db.instance.id differs across probe instances: %q vs %q", id1, id3)
	}
}

func TestIntegration_InstanceNameOverride(t *testing.T) {
	tg := itTargetFromEnv(t)
	params := tg.params()
	params["instance_name"] = "it-fixed-name"
	p := itStartParams(t, params)
	_ = itCollect(t, p)

	if id := itObserveID(t, p); id != "it-fixed-name" {
		t.Errorf("db.instance.id = %q, want it-fixed-name", id)
	}
}

func TestIntegration_WrongPasswordIsRefusedCredentials(t *testing.T) {
	tg := itTargetFromEnv(t)
	tg.password += "-wrong"
	p := itStart(t, tg)
	points := itCollect(t, p)

	if up := itOne(t, points, "senhub.db.up"); up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}
	if check := itOne(t, points, dbcommon.CheckResultMetric); check.Value != float64(dbcommon.CheckAuthDenied) {
		t.Errorf("senhub.probe.check.result = %v, want %v", check.Value, float64(dbcommon.CheckAuthDenied))
	}
	obs, ok := p.entitySrc.Observe()
	if ok {
		t.Errorf("Observe ok=true with wrong password, want false")
	}
	if len(obs.Entities) != 0 {
		t.Errorf("Observe emitted %d entities, want 0", len(obs.Entities))
	}
}

func TestIntegration_UnreachableIsTargetDown(t *testing.T) {
	tg := itTargetFromEnv(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("closing the probe listener: %v", err)
	}

	params := tg.params()
	params["host"] = "127.0.0.1"
	params["port"] = port
	p := itStartParams(t, params)
	points := itCollect(t, p)

	if up := itOne(t, points, "senhub.db.up"); up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}
	if check := itOne(t, points, dbcommon.CheckResultMetric); check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("senhub.probe.check.result = %v, want %v", check.Value, float64(dbcommon.CheckTargetDown))
	}
}

func itReplicaTarget(t *testing.T) itTarget {
	t.Helper()
	tg := itTargetFromEnv(t)
	v := os.Getenv("MYSQL_REPLICA_TEST_PORT")
	if v == "" {
		t.Skip("no replica to test against: set MYSQL_REPLICA_TEST_PORT")
	}
	port, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("MYSQL_REPLICA_TEST_PORT %q is not a number", v)
	}
	tg.port = port
	return tg
}

func itCollectUntil(t *testing.T, p *mysqlProbe, what string, ok func([]data_store.DataPoint) bool) []data_store.DataPoint {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		points := itCollect(t, p)
		if ok(points) {
			return points
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Second)
	}
}

func TestIntegration_Replication_SourceWithReplica(t *testing.T) {
	itReplicaTarget(t)
	tg := itTargetFromEnv(t)
	p := itStart(t, tg)

	points := itCollectUntil(t, p, "the source to report a connected replica", func(pts []data_store.DataPoint) bool {
		got := itNamed(pts, "senhub.db.replication.replicas.connected")
		return len(got) == 1 && got[0].Value >= 1
	})

	if n := itOne(t, points, "senhub.db.replication.replicas.connected").Value; n != 1 {
		t.Errorf("senhub.db.replication.replicas.connected = %v, want 1", n)
	}
	role := itOne(t, points, "senhub.db.replication.role")
	if itTag(role, "role") != "primary" {
		t.Errorf("senhub.db.replication.role tag role = %q, want primary", itTag(role, "role"))
	}
	if role.Value != dbcommon.RolePrimary.RoleValue() {
		t.Errorf("senhub.db.replication.role = %v, want %v", role.Value, dbcommon.RolePrimary.RoleValue())
	}
	if health := itOne(t, points, "senhub.db.replication.health"); health.Value != 1 {
		t.Errorf("senhub.db.replication.health = %v, want 1", health.Value)
	}
}

func TestIntegration_Replication_Replica(t *testing.T) {
	tg := itReplicaTarget(t)
	p := itStart(t, tg)

	points := itCollectUntil(t, p, "the replica threads to run", func(pts []data_store.DataPoint) bool {
		io := itNamed(pts, "senhub.db.mysql.replica.io_thread.running")
		sqlT := itNamed(pts, "senhub.db.mysql.replica.sql_thread.running")
		return len(io) == 1 && io[0].Value == 1 && len(sqlT) == 1 && sqlT[0].Value == 1
	})

	role := itOne(t, points, "senhub.db.replication.role")
	if itTag(role, "role") != "replica" {
		t.Errorf("senhub.db.replication.role tag role = %q, want replica", itTag(role, "role"))
	}
	if role.Value != dbcommon.RoleReplica.RoleValue() {
		t.Errorf("senhub.db.replication.role = %v, want %v", role.Value, dbcommon.RoleReplica.RoleValue())
	}
	if lag := itOne(t, points, "mysql.replica.time_behind_source"); lag.Value < 0 {
		t.Errorf("mysql.replica.time_behind_source = %v, want >= 0", lag.Value)
	}
	if health := itOne(t, points, "senhub.db.replication.health"); health.Value != 1 {
		t.Errorf("senhub.db.replication.health = %v, want 1", health.Value)
	}
	if got := len(itNamed(points, "senhub.db.replication.replicas.connected")); got != 0 {
		t.Errorf("replica emitted %d replicas.connected series, want 0", got)
	}
}
