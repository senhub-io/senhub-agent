//go:build database_integration

// Integration tests against a live MySQL/MariaDB server. Gated by
// the database_integration build tag so the standard `make test`
// stays fast (and doesn't require Docker on the developer's box).
//
// Run with:
//
//   MYSQL_TEST_DSN='root:test@tcp(127.0.0.1:3306)/' \
//     go test -tags=database_integration ./internal/agent/probes/mysql/...
//
// or via `make test-database` which spins up the docker-compose
// fixture and points MYSQL_TEST_DSN at it.

package mysql

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func envDSN(t *testing.T) string {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set — skipping integration test")
	}
	return dsn
}

// parseTestDSN extracts host, port, user, password from a DSN of
// the form `user:pass@tcp(host:port)/`. Cheap regex-free parser
// since we control the test input shape.
func parseTestDSN(dsn string) (user, pass, host string, port int) {
	at := strings.Index(dsn, "@tcp(")
	if at == -1 {
		return
	}
	creds := dsn[:at]
	if colon := strings.Index(creds, ":"); colon >= 0 {
		user = creds[:colon]
		pass = creds[colon+1:]
	} else {
		user = creds
	}
	rest := dsn[at+len("@tcp("):]
	if end := strings.Index(rest, ")"); end >= 0 {
		hp := rest[:end]
		if colon := strings.Index(hp, ":"); colon >= 0 {
			host = hp[:colon]
			_, _ = fmtSscan(hp[colon+1:], &port)
		} else {
			host = hp
			port = 3306
		}
	}
	return
}

// fmtSscan is fmt.Sscan as a no-allocation helper avoiding a fmt
// import cycle in tests when not otherwise needed.
func fmtSscan(s string, out *int) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return 1, nil
}

func TestIntegration_ConnectAndOverview(t *testing.T) {
	dsn := envDSN(t)
	user, pass, host, port := parseTestDSN(dsn)
	if host == "" {
		t.Fatalf("could not parse MYSQL_TEST_DSN=%q", dsn)
	}

	cfg := map[string]interface{}{
		"host":     host,
		"port":     port,
		"username": user,
		"password": pass,
		"interval": 30,
		"timeout":  5,
	}
	probe, err := NewMySQLProbe(cfg, nil)
	if err != nil {
		t.Fatalf("NewMySQLProbe: %v", err)
	}
	defer probe.OnShutdown(context.Background())

	if err := probe.OnStart(make(chan struct{})); err != nil {
		t.Fatalf("OnStart: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Find the metrics every healthy MySQL must produce.
	names := map[string]float32{}
	for _, p := range points {
		names[p.Name] = p.Value
	}

	mustHave := []string{
		"db_up", "db_uptime_seconds", "db_version_info",
		"db_connections_active", "db_connections_max",
		"db_queries_count", "db_transactions_committed",
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
	if v := names["db_buffer_hit_ratio"]; v < 0 || v > 1 {
		t.Errorf("db_buffer_hit_ratio = %v, must be in [0,1]", v)
	}
}

func TestIntegration_PingMatrix(t *testing.T) {
	// Quick connectivity smoke test that runs even if the probe
	// surface changes. Catches DSN-building regressions.
	dsn := envDSN(t)
	db, err := sql.Open("mysql", dsn)
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
