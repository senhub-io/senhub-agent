//go:build integration

package postgresql

import (
	"database/sql"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type itTarget struct {
	host     string
	port     int
	username string
	password string
}

func itTargetFromEnv(t *testing.T) itTarget {
	t.Helper()
	host := os.Getenv("POSTGRESQL_TEST_HOST")
	user := os.Getenv("POSTGRESQL_TEST_USER")
	if host == "" || user == "" {
		t.Skip("no PostgreSQL to test against: set POSTGRESQL_TEST_HOST/USER/PASSWORD")
	}
	port := 5432
	if v := os.Getenv("POSTGRESQL_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("POSTGRESQL_TEST_PORT %q is not a number", v)
		}
		port = p
	}
	return itTarget{
		host:     host,
		port:     port,
		username: user,
		password: os.Getenv("POSTGRESQL_TEST_PASSWORD"),
	}
}

func itLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func itStart(t *testing.T, tg itTarget) *pgProbe {
	t.Helper()
	p, err := NewPostgreSQLProbe(map[string]interface{}{
		"host":     tg.host,
		"port":     tg.port,
		"username": tg.username,
		"password": tg.password,
	}, itLogger())
	if err != nil {
		t.Fatalf("NewPostgreSQLProbe: %v", err)
	}
	op := p.(*pgProbe)
	op.SetName("integration-postgresql")
	if err := op.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		if err := op.OnShutdown(nil); err != nil {
			t.Errorf("OnShutdown: %v", err)
		}
	})
	return op
}

func itCollect(t *testing.T, op *pgProbe) []data_store.DataPoint {
	t.Helper()
	points, err := op.Collect()
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

func TestIntegration_Collect_Healthy(t *testing.T) {
	tg := itTargetFromEnv(t)
	op := itStart(t, tg)

	points := itCollect(t, op)

	if up := itOne(t, points, "senhub.db.up"); up.Value != 1 {
		t.Errorf("senhub.db.up = %v, want 1; probe could not log in as %s to %s:%d", up.Value, tg.username, tg.host, tg.port)
	}
	if chk := itOne(t, points, dbcommon.CheckResultMetric); chk.Value != float64(dbcommon.CheckOK) {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, chk.Value, float64(dbcommon.CheckOK))
	}
	if uptime := itOne(t, points, "senhub.db.postgresql.uptime"); uptime.Value <= 0 {
		t.Errorf("senhub.db.postgresql.uptime = %v, want > 0", uptime.Value)
	}

	backends := itNamed(points, "postgresql.backends")
	var backendSum float64
	for _, dp := range backends {
		backendSum += dp.Value
	}
	if len(backends) == 0 || backendSum < 1 {
		t.Errorf("postgresql.backends: got %d series with sum %v, want at least one and sum >= 1", len(backends), backendSum)
	}
	if v := itOne(t, points, "postgresql.connection.max").Value; v <= 0 {
		t.Errorf("postgresql.connection.max = %v, want > 0", v)
	}
	if versionInfo := itOne(t, points, "senhub.db.version.info"); itTag(versionInfo, "version") == "" {
		t.Errorf("senhub.db.version.info emitted without a non-empty version tag")
	}
	for _, name := range []string{
		"postgresql.commits",
		"postgresql.rollbacks",
		"postgresql.db_size",
		"postgresql.table.count",
		"senhub.db.replication.role",
		"senhub.db.replication.health",
	} {
		if len(itNamed(points, name)) == 0 {
			t.Errorf("%s not emitted", name)
		}
	}

	role := itOne(t, points, "senhub.db.replication.role")
	if itTag(role, "role") != "standalone" {
		t.Errorf("senhub.db.replication.role tag role = %q, want standalone", itTag(role, "role"))
	}
	if role.Value != 0 {
		t.Errorf("senhub.db.replication.role = %v, want 0 for standalone", role.Value)
	}
	if health := itOne(t, points, "senhub.db.replication.health"); health.Value != 1 {
		t.Errorf("senhub.db.replication.health = %v, want 1", health.Value)
	}
}

func TestIntegration_Entity(t *testing.T) {
	tg := itTargetFromEnv(t)
	op := itStart(t, tg)

	points := itCollect(t, op)

	obs, ok := op.entitySrc.Observe()
	if !ok {
		t.Fatalf("Observe() ok=false after a successful collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entity count = %d, want 1", len(obs.Entities))
	}
	ent := obs.Entities[0]
	if ent.Type != "db" {
		t.Errorf("entity type = %q, want db", ent.Type)
	}

	rawID, ok := ent.ID["db.instance.id"]
	if !ok {
		t.Fatalf("entity ID missing db.instance.id")
	}
	id, ok := rawID.(string)
	if !ok {
		t.Fatalf("db.instance.id is not a string: %T", rawID)
	}
	if id == "" {
		t.Errorf("db.instance.id is empty")
	}
	if !strings.HasPrefix(id, "postgresql:") {
		t.Errorf("db.instance.id = %q, want prefix postgresql:", id)
	}

	if v, _ := ent.Attributes["db.system.name"].(string); v != "postgresql" {
		t.Errorf("entity attribute db.system.name = %q, want postgresql", v)
	}
	if v, _ := ent.Attributes["server.address"].(string); v != tg.host {
		t.Errorf("entity attribute server.address = %q, want %q", v, tg.host)
	}
	if v, _ := ent.Attributes["db.system.version"].(string); v == "" {
		t.Errorf("entity attribute db.system.version is empty")
	}
	if v, _ := ent.Attributes["replication.role"].(string); v != "standalone" {
		t.Errorf("entity attribute replication.role = %q, want standalone", v)
	}

	var tagCarriers int
	for _, dp := range points {
		got := itTag(dp, "db.instance.id")
		if got == "" {
			continue
		}
		tagCarriers++
		if got != id {
			t.Errorf("%s carries db.instance.id %q, want entity ID %q", dp.Name, got, id)
		}
	}
	if tagCarriers == 0 {
		t.Errorf("no datapoint carries db.instance.id")
	}
}

func TestIntegration_IdentityStableAcrossCollections(t *testing.T) {
	tg := itTargetFromEnv(t)
	op := itStart(t, tg)

	itCollect(t, op)
	id1 := op.entitySrc.currentDBInstanceID()
	if id1 == "" {
		t.Fatalf("expected a pinned db.instance.id after first collect")
	}

	itCollect(t, op)
	id2 := op.entitySrc.currentDBInstanceID()
	if id2 != id1 {
		t.Errorf("db.instance.id changed across collections: %q -> %q", id1, id2)
	}

	op2 := itStart(t, tg)
	itCollect(t, op2)
	id3 := op2.entitySrc.currentDBInstanceID()
	if id3 != id1 {
		t.Errorf("second probe db.instance.id = %q, want %q", id3, id1)
	}
}

func TestIntegration_InstanceNameOverride(t *testing.T) {
	tg := itTargetFromEnv(t)
	params := map[string]interface{}{
		"host":          tg.host,
		"port":          tg.port,
		"username":      tg.username,
		"password":      tg.password,
		"instance_name": "it-fixed-name",
	}

	p, err := NewPostgreSQLProbe(params, itLogger())
	if err != nil {
		t.Fatalf("NewPostgreSQLProbe: %v", err)
	}
	op := p.(*pgProbe)
	op.SetName("integration-postgresql-instance-name")
	if err := op.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		if err := op.OnShutdown(nil); err != nil {
			t.Errorf("OnShutdown: %v", err)
		}
	})

	points := itCollect(t, op)

	if got := op.entitySrc.currentDBInstanceID(); got != "it-fixed-name" {
		t.Errorf("currentDBInstanceID = %q, want it-fixed-name", got)
	}
	if got := itTag(itOne(t, points, "senhub.db.up"), "db.instance.id"); got != "it-fixed-name" {
		t.Errorf("senhub.db.up db.instance.id tag = %q, want it-fixed-name", got)
	}

	obs, ok := op.entitySrc.Observe()
	if !ok {
		t.Fatalf("Observe() ok=false with instance_name override")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entity count = %d, want 1", len(obs.Entities))
	}
	if got, _ := obs.Entities[0].ID["db.instance.id"].(string); got != "it-fixed-name" {
		t.Errorf("entity db.instance.id = %q, want it-fixed-name", got)
	}
}

func TestIntegration_ReplicaRoleQueryOnPrimary(t *testing.T) {
	tg := itTargetFromEnv(t)

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(tg.username, tg.password),
		Host:     net.JoinHostPort(tg.host, strconv.Itoa(tg.port)),
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the check connection: %v", err)
		}
	})

	var recovery bool
	if err := db.QueryRow(`SELECT pg_is_in_recovery()`).Scan(&recovery); err != nil {
		t.Fatalf("query pg_is_in_recovery: %v", err)
	}
	if recovery {
		t.Errorf("pg_is_in_recovery() = true, want false")
	}

	op := itStart(t, tg)
	itCollect(t, op)

	obs, ok := op.entitySrc.Observe()
	if !ok {
		t.Fatalf("Observe() ok=false after collect")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("entity count = %d, want 1", len(obs.Entities))
	}
	if role, _ := obs.Entities[0].Attributes["replication.role"].(string); role == "replica" {
		t.Errorf("entity replication.role = %q, want not replica", role)
	}
}

func TestIntegration_WrongPasswordIsRefusedCredentials(t *testing.T) {
	tg := itTargetFromEnv(t)
	tg.password += "-wrong"
	op := itStart(t, tg)

	points, err := op.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if up := itOne(t, points, "senhub.db.up"); up.Value != 0 {
		t.Errorf("senhub.db.up = %v with wrong password, want 0", up.Value)
	}
	if chk := itOne(t, points, dbcommon.CheckResultMetric); chk.Value != float64(dbcommon.CheckAuthDenied) {
		t.Errorf("%s = %v with wrong password, want %v", dbcommon.CheckResultMetric, chk.Value, float64(dbcommon.CheckAuthDenied))
	}
	if _, ok := op.entitySrc.Observe(); ok {
		t.Errorf("Observe() ok=true with wrong password, want false")
	}
}

func TestIntegration_UnreachableIsTargetDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	params := map[string]interface{}{
		"host":     "127.0.0.1",
		"port":     port,
		"username": "postgres",
		"password": "postgres",
	}
	p, err := NewPostgreSQLProbe(params, itLogger())
	if err != nil {
		t.Fatalf("NewPostgreSQLProbe: %v", err)
	}
	op := p.(*pgProbe)
	op.SetName("integration-postgresql-unreachable")
	if err := op.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() {
		if err := op.OnShutdown(nil); err != nil {
			t.Errorf("OnShutdown: %v", err)
		}
	})

	points, err := op.Collect()
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if up := itOne(t, points, "senhub.db.up"); up.Value != 0 {
		t.Errorf("senhub.db.up = %v against unreachable target, want 0", up.Value)
	}
	if chk := itOne(t, points, dbcommon.CheckResultMetric); chk.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%s = %v against unreachable target, want %v", dbcommon.CheckResultMetric, chk.Value, float64(dbcommon.CheckTargetDown))
	}
}
