//go:build database_integration

// Integration tests against a live PostgreSQL server. Gated by
// the database_integration build tag (same convention as the
// mysql probe).
//
// Run with:
//
//   POSTGRES_TEST_DSN='host=127.0.0.1 port=5432 user=postgres password=test dbname=postgres sslmode=disable' \
//     go test -tags=database_integration ./internal/agent/probes/postgresql/...

package postgresql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func envDSN(t *testing.T) string {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set — skipping integration test")
	}
	return dsn
}

// libpqValue scans `key=value` pairs out of a libpq-style DSN.
func libpqValue(dsn, key string) string {
	for _, part := range strings.Fields(dsn) {
		if strings.HasPrefix(part, key+"=") {
			return part[len(key)+1:]
		}
	}
	return ""
}

func TestIntegration_ConnectAndOverview(t *testing.T) {
	dsn := envDSN(t)
	host := libpqValue(dsn, "host")
	port := 5432
	if p := libpqValue(dsn, "port"); p != "" {
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			port = port*10 + int(c-'0') // unused; just keep simple
		}
		// Re-parse cleanly
		port = 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			port = port*10 + int(c-'0')
		}
	}
	user := libpqValue(dsn, "user")
	pass := libpqValue(dsn, "password")
	db := libpqValue(dsn, "dbname")
	if db == "" {
		db = "postgres"
	}
	sslmode := libpqValue(dsn, "sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}

	cfg := map[string]interface{}{
		"host":     host,
		"port":     port,
		"username": user,
		"password": pass,
		"database": db,
		"sslmode":  sslmode,
		"interval": 30,
		"timeout":  5,
	}
	probe, err := NewPostgreSQLProbe(cfg, nil)
	if err != nil {
		t.Fatalf("NewPostgreSQLProbe: %v", err)
	}
	defer probe.OnShutdown(context.Background())

	if err := probe.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	names := map[string]float32{}
	for _, p := range points {
		names[p.Name] = p.Value
	}

	mustHave := []string{
		"db_up", "db_uptime_seconds", "db_version_info",
		"db_connections_active",
		"db_transactions_committed",
		"db_buffer_hit_ratio", "db_size_bytes",
	}
	for _, n := range mustHave {
		if _, ok := names[n]; !ok {
			t.Errorf("missing metric %q (got %d distinct names)", n, len(names))
		}
	}

	if v := names["db_up"]; v != 1 {
		t.Errorf("db_up = %v, want 1", v)
	}
	if v := names["db_uptime_seconds"]; v <= 0 {
		t.Errorf("db_uptime_seconds = %v, expected > 0", v)
	}
	if v, ok := names["db_buffer_hit_ratio"]; ok && (v < 0 || v > 1) {
		t.Errorf("db_buffer_hit_ratio = %v, must be in [0,1]", v)
	}
}

func TestIntegration_PingMatrix(t *testing.T) {
	dsn := envDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
