package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

const collectGoldenPath = "testdata/collect.golden"

type goldenResult struct {
	cols []string
	rows [][]driver.Value
}

type goldenServer struct {
	results  map[string]goldenResult
	pingFail bool
}

func (f *goldenServer) Connect(context.Context) (driver.Conn, error) {
	if f.pingFail {
		return nil, errors.New("connection refused")
	}
	return &goldenConn{f}, nil
}
func (f *goldenServer) Driver() driver.Driver { return goldenDriver{} }

type goldenDriver struct{}

func (goldenDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type goldenConn struct{ srv *goldenServer }

func (c *goldenConn) Prepare(q string) (driver.Stmt, error) { return &goldenStmt{c.srv, q}, nil }
func (c *goldenConn) Close() error                          { return nil }
func (c *goldenConn) Begin() (driver.Tx, error)             { return nil, errors.New("unused") }

type goldenStmt struct {
	srv *goldenServer
	q   string
}

func (s *goldenStmt) Close() error  { return nil }
func (s *goldenStmt) NumInput() int { return -1 }
func (s *goldenStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("unused")
}
func (s *goldenStmt) Query([]driver.Value) (driver.Rows, error) {
	r, ok := s.srv.results[s.q]
	if !ok {
		return nil, fmt.Errorf("unknown query %q", s.q)
	}
	return &goldenRows{r, 0}, nil
}

type goldenRows struct {
	goldenResult
	i int
}

func (r *goldenRows) Columns() []string { return r.cols }
func (r *goldenRows) Close() error      { return nil }
func (r *goldenRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

func goldenKV(pairs ...string) goldenResult {
	r := goldenResult{cols: []string{"Variable_name", "Value"}}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.rows = append(r.rows, []driver.Value{pairs[i], pairs[i+1]})
	}
	return r
}

func goldenBaseResults() map[string]goldenResult {
	return map[string]goldenResult{
		serverUUIDQuery: {cols: []string{"uuid"}, rows: [][]driver.Value{{"11111111-2222-3333-4444-555555555555"}}},
		statusQuery: goldenKV("Uptime", "1000", "Threads_connected", "10", "Threads_running", "3",
			"Aborted_clients", "1", "Aborted_connects", "2", "Connection_errors_max_connections", "3",
			"Questions", "500", "Slow_queries", "4", "Com_select", "100", "Com_insert", "20", "Com_update", "30", "Com_delete", "5", "Com_replace", "1",
			"Com_commit", "60", "Com_rollback", "6", "Created_tmp_tables", "50", "Created_tmp_disk_tables", "5",
			"Innodb_buffer_pool_reads", "10", "Innodb_buffer_pool_read_requests", "1000",
			"Innodb_buffer_pool_pages_total", "200", "Innodb_buffer_pool_pages_data", "150", "Innodb_buffer_pool_pages_dirty", "7",
			"Innodb_deadlocks", "2", "Innodb_row_lock_current_waits", "1", "Innodb_row_lock_time_avg", "12",
			"Innodb_data_read", "4096", "Innodb_data_written", "8192"),
		variablesQuery:       goldenKV("max_connections", "151", "version", "8.0.36", "version_comment", "MySQL Community Server - GPL"),
		totalSizeQuery:       {cols: []string{"s"}, rows: [][]driver.Value{{float64(123456)}}},
		tableCountQuery:      {cols: []string{"c"}, rows: [][]driver.Value{{int64(42)}}},
		replicaStatusQuery:   {cols: []string{"Slave_IO_Running"}},
		replicaCountQuery:    {cols: []string{"Server_id"}},
		perDatabaseSizeQuery: {cols: []string{"db", "size"}, rows: [][]driver.Value{{"app", float64(1000)}, {"shop", float64(2000)}}},
		perTableSizeQuery: {cols: []string{"db", "t", "size"}, rows: [][]driver.Value{
			{"app", "users", float64(300)}, {"shop", "orders", float64(900)}, {"shop", "items", float64(100)}}},
	}
}

func renderGoldenCollect(pts []data_store.DataPoint) string {
	var lines []string
	flush := func(run []string, sb *strings.Builder) {
		sort.Strings(run)
		for _, l := range run {
			sb.WriteString(l + "\n")
		}
	}
	var sb strings.Builder
	var run []string
	prev := ""
	for _, dp := range pts {
		if dp.Name != prev {
			flush(run, &sb)
			run, prev = nil, dp.Name
		}
		tg := make([]string, 0, len(dp.Tags))
		for _, tag := range dp.Tags {
			tg = append(tg, tag.Key+"="+tag.Value)
		}
		run = append(run, fmt.Sprintf("%s | %g | %s", dp.Name, dp.Value, strings.Join(tg, ",")))
	}
	flush(run, &sb)
	_ = lines
	return sb.String()
}

func TestCollect_GoldenOutput(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(p *mysqlProbe, srv *goldenServer)
	}{
		{"standalone", func(p *mysqlProbe, srv *goldenServer) {}},
		{"primary-per-db-per-table", func(p *mysqlProbe, srv *goldenServer) {
			p.cfg.PerDatabase, p.cfg.PerTable, p.cfg.TopNTables = true, true, 2
			srv.results[replicaCountQuery] = goldenResult{cols: []string{"Server_id"}, rows: [][]driver.Value{{int64(2)}, {int64(3)}}}
		}},
		{"primary-no-replicas-connected", func(p *mysqlProbe, srv *goldenServer) {
			p.cfg.PerDatabase = true
		}},
		{"replica-healthy", func(p *mysqlProbe, srv *goldenServer) {
			p.cfg.MaxReplicationLag = 300 * time.Second
			srv.results[replicaStatusQuery] = goldenResult{
				cols: []string{"Slave_IO_Running", "Slave_SQL_Running", "Seconds_Behind_Master"},
				rows: [][]driver.Value{{"Yes", "Yes", int64(5)}},
			}
		}},
		{"replica-lagging", func(p *mysqlProbe, srv *goldenServer) {
			p.cfg.MaxReplicationLag = 10 * time.Second
			srv.results[replicaStatusQuery] = goldenResult{
				cols: []string{"Slave_IO_Running", "Slave_SQL_Running", "Seconds_Behind_Master"},
				rows: [][]driver.Value{{"Yes", "Yes", int64(500)}},
			}
		}},
		{"no-storage-no-variables", func(p *mysqlProbe, srv *goldenServer) {
			delete(srv.results, totalSizeQuery)
			delete(srv.results, tableCountQuery)
			delete(srv.results, variablesQuery)
			delete(srv.results, replicaStatusQuery)
			srv.results[replicaStatusFallback] = goldenResult{cols: []string{"x"}}
			delete(srv.results, replicaCountQuery)
			srv.results[replicaCountFallback] = goldenResult{cols: []string{"x"}}
		}},
		{"status-fails", func(p *mysqlProbe, srv *goldenServer) { delete(srv.results, statusQuery) }},
		{"ping-fails", func(p *mysqlProbe, srv *goldenServer) { srv.pingFail = true }},
		{"no-pool", func(p *mysqlProbe, srv *goldenServer) { p.db = nil }},
	}

	var sb strings.Builder
	for _, s := range scenarios {
		raw, err := NewMysqlProbe(map[string]interface{}{"host": "db1", "port": 3306}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
		if err != nil {
			t.Fatal(err)
		}
		p := raw.(*mysqlProbe)
		p.SetName("mysql-test")
		p.entitySrc.hostID = func() string { return "test-host" }
		p.entitySrc.hostID = func() string { return "test-host" }
		srv := &goldenServer{results: goldenBaseResults()}
		p.db = sql.OpenDB(srv)
		s.setup(p, srv)
		pts, cerr := p.Collect()
		fmt.Fprintf(&sb, "===== %s (err=%v) =====\n%s", s.name, cerr, renderGoldenCollect(pts))
		if p.db != nil {
			_ = p.db.Close()
		}
	}
	got := sb.String()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(collectGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(collectGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(collectGoldenPath)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("Collect output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
