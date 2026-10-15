package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net"
	"sort"
	"sync/atomic"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
)

type fakeMySQLConn struct {
	pingErr error
	status  map[string]string
	vars    map[string]string
	uuid    string
}

func (c *fakeMySQLConn) Ping(ctx context.Context) error            { return c.pingErr }
func (c *fakeMySQLConn) Close() error                              { return nil }
func (c *fakeMySQLConn) Begin() (driver.Tx, error)                 { return nil, nil }
func (c *fakeMySQLConn) Prepare(query string) (driver.Stmt, error) { return nil, nil }

func (c *fakeMySQLConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case statusQuery:
		return newKVRows(c.status), nil
	case variablesQuery:
		return newKVRows(c.vars), nil
	case serverUUIDQuery:
		return newSingleStringRows(c.uuid), nil
	case replicaStatusQuery, replicaStatusFallback, replicaCountQuery, replicaCountFallback:
		return &fakeRows{cols: []string{}}, nil
	case totalSizeQuery, tableCountQuery:
		return newSingleValueRows(100.0), nil
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
}

type fakeMySQLDriver struct {
	conn *fakeMySQLConn
}

func (d *fakeMySQLDriver) Open(_ string) (driver.Conn, error) { return d.conn, nil }

type fakeRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func newKVRows(m map[string]string) driver.Rows {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]driver.Value, 0, len(m))
	for _, k := range keys {
		rows = append(rows, []driver.Value{k, m[k]})
	}
	return &fakeRows{cols: []string{"Variable_name", "Value"}, rows: rows}
}

func newSingleValueRows(v float64) driver.Rows {
	return &fakeRows{cols: []string{"value"}, rows: [][]driver.Value{{v}}}
}

func newSingleStringRows(v string) driver.Rows {
	return &fakeRows{cols: []string{"value"}, rows: [][]driver.Value{{v}}}
}

// fakeDriverSeq names each fake driver: sql.Register panics on a reused
// name, and the Windows clock is too coarse for a timestamp to be unique.
var fakeDriverSeq atomic.Int64

func newProbeWithFakeDB(t *testing.T, conn *fakeMySQLConn) *mysqlProbe {
	t.Helper()

	p := newProbeForTest(t, map[string]interface{}{
		"host":     "127.0.0.1",
		"port":     3306,
		"username": "u",
		"password": "p",
	})

	driverName := fmt.Sprintf("mysql-fake-%d", fakeDriverSeq.Add(1))
	sql.Register(driverName, &fakeMySQLDriver{conn: conn})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	p.db = db
	p.entitySrc.hostID = func() string { return "test-host" }
	return p
}

func fakeStatusMap() map[string]string {
	return map[string]string{
		"Uptime":                            "100",
		"Threads_connected":                 "5",
		"Threads_running":                   "2",
		"Aborted_clients":                   "0",
		"Aborted_connects":                  "0",
		"Connection_errors_max_connections": "0",
		"Questions":                         "10",
		"Slow_queries":                      "0",
		"Com_select":                        "8",
		"Com_insert":                        "1",
		"Com_update":                        "1",
		"Com_delete":                        "0",
		"Com_replace":                       "0",
		"Com_commit":                        "5",
		"Com_rollback":                      "0",
		"Created_tmp_tables":                "1",
		"Created_tmp_disk_tables":           "0",
		"Innodb_buffer_pool_reads":          "1",
		"Innodb_buffer_pool_read_requests":  "100",
		"Innodb_buffer_pool_pages_total":    "1000",
		"Innodb_buffer_pool_pages_data":     "900",
		"Innodb_buffer_pool_pages_dirty":    "10",
		"Innodb_deadlocks":                  "0",
		"Innodb_row_lock_current_waits":     "0",
		"Innodb_row_lock_time_avg":          "0",
		"Innodb_data_read":                  "0",
		"Innodb_data_written":               "0",
	}
}

func fakeVarsMap() map[string]string {
	return map[string]string{
		"max_connections": "151",
		"version":         "8.0.36",
		"version_comment": "MySQL Community Server",
	}
}

func findPoint(points []data_store.DataPoint, name string) *data_store.DataPoint {
	for i := range points {
		if points[i].Name == name {
			return &points[i]
		}
	}
	return nil
}

func tagValue(dp data_store.DataPoint, key string) string {
	for _, t := range dp.Tags {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

func TestCollectSuccessCarriesEntityIDAndCheckResultOK(t *testing.T) {
	conn := &fakeMySQLConn{
		uuid:   "uuid-123",
		status: fakeStatusMap(),
		vars:   fakeVarsMap(),
	}
	p := newProbeWithFakeDB(t, conn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints")
	}

	wantID := "mysql:uuid-123"
	for _, dp := range points {
		if got := tagValue(dp, "db.instance.id"); got != wantID {
			t.Errorf("%s: db.instance.id = %q, want %q", dp.Name, got, wantID)
		}
	}

	cr := findPoint(points, dbcommon.CheckResultMetric)
	if cr == nil {
		t.Fatalf("missing %s", dbcommon.CheckResultMetric)
	}
	if cr.Value != 0 {
		t.Errorf("%s = %v, want 0", dbcommon.CheckResultMetric, cr.Value)
	}
	if got := tagValue(*cr, "metric_type"); got != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, got)
	}
	if got := tagValue(*cr, "db.instance.id"); got != wantID {
		t.Errorf("%s db.instance.id = %q, want %q", dbcommon.CheckResultMetric, got, wantID)
	}
}

func TestCollectFailedConnectionUsesFallbackIDAndClassifies(t *testing.T) {
	pingErr := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	conn := &fakeMySQLConn{pingErr: pingErr}
	p := newProbeWithFakeDB(t, conn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	up := findPoint(points, "senhub.db.up")
	if up == nil {
		t.Fatal("missing senhub.db.up")
	}
	if up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}

	cr := findPoint(points, dbcommon.CheckResultMetric)
	if cr == nil {
		t.Fatalf("missing %s", dbcommon.CheckResultMetric)
	}
	wantClass := float64(dbcommon.ClassifyError(pingErr))
	if cr.Value != wantClass {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, cr.Value, wantClass)
	}
	if got := tagValue(*cr, "metric_type"); got != "status" {
		t.Errorf("%s metric_type = %q, want status", dbcommon.CheckResultMetric, got)
	}

	// Never reached, so no entity exists yet: a key here would name nothing.
	for _, dp := range points {
		if got := tagValue(dp, "db.instance.id"); got != "" {
			t.Errorf("%s: db.instance.id = %q before any entity, want none", dp.Name, got)
		}
	}
}
