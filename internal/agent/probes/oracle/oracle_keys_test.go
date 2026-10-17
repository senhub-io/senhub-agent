package oracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type fakeOracleConnector struct {
	pingErr error
}

func (c *fakeOracleConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeOracleConn{pingErr: c.pingErr}, nil
}

func (c *fakeOracleConnector) Driver() driver.Driver {
	return fakeOracleDriver{}
}

type fakeOracleDriver struct{}

func (fakeOracleDriver) Open(string) (driver.Conn, error) {
	return &fakeOracleConn{}, nil
}

type fakeOracleConn struct {
	pingErr error
}

func (c *fakeOracleConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *fakeOracleConn) Close() error { return nil }

func (c *fakeOracleConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin not supported")
}

func (c *fakeOracleConn) Ping(context.Context) error { return c.pingErr }

func (c *fakeOracleConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	switch {
	case strings.Contains(q, "from v$instance"):
		return makeRows([]string{"version"}, [][]driver.Value{{"19.0.0.0.0"}}), nil
	case strings.Contains(q, "from v$session"):
		return makeRows([]string{"status", "count"}, [][]driver.Value{
			{"ACTIVE", float64(5)},
			{"INACTIVE", float64(10)},
		}), nil
	case strings.Contains(q, "from v$resource_limit"):
		return makeRows([]string{"limit_value"}, [][]driver.Value{{float64(200)}}), nil
	case strings.Contains(q, "from v$sysstat"):
		return makeRows([]string{"name", "value"}, [][]driver.Value{
			{"physical reads", float64(100)},
			{"physical writes", float64(200)},
			{"consistent gets", float64(1000)},
			{"db block gets", float64(500)},
			{"enqueue deadlocks", float64(2)},
		}), nil
	case strings.Contains(q, "from v$sgastat"):
		return makeRows([]string{"SUM(bytes)"}, [][]driver.Value{{float64(1024)}}), nil
	case strings.Contains(q, "from v$pgastat"):
		return makeRows([]string{"value"}, [][]driver.Value{{float64(512)}}), nil
	case strings.Contains(q, "from dba_tablespace_usage_metrics"):
		return makeRows([]string{"tablespace_name", "used", "total"}, [][]driver.Value{
			{"SYSTEM", float64(1024), float64(2048)},
		}), nil
	case strings.Contains(q, "from v$system_wait_class"):
		return makeRows([]string{"wait_class", "time_waited"}, [][]driver.Value{
			{"USER I/O", float64(123)},
		}), nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type fakeRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func makeRows(cols []string, rows [][]driver.Value) driver.Rows {
	return &fakeRows{cols: cols, rows: rows}
}

func (r *fakeRows) Columns() []string { return r.cols }

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func newTestOracleProbe(t *testing.T) *oracleProbe {
	t.Helper()
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
		"password":     "secret",
		"port":         1521,
	}
	pi, err := NewOracleProbe(raw, &logger.Logger{})
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	probe, ok := pi.(*oracleProbe)
	if !ok {
		t.Fatalf("NewOracleProbe returned %T, want *oracleProbe", pi)
	}
	return probe
}

func tagValue(tags []tags.Tag, key string) (string, bool) {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func mustPoint(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	for _, dp := range points {
		if dp.Name == name {
			return dp
		}
	}
	t.Fatalf("datapoint %q not found", name)
	return data_store.DataPoint{}
}

func assertIdentityKeys(t *testing.T, points []data_store.DataPoint, want string) {
	t.Helper()
	for _, dp := range points {
		if got, ok := tagValue(dp.Tags, idKeyDBInstance); !ok {
			t.Errorf("%s missing identity tag %s", dp.Name, idKeyDBInstance)
		} else if got != want {
			t.Errorf("%s identity tag = %q, want %q", dp.Name, got, want)
		}
	}
}

func TestOracleCollect_KeysAndCheckResultSuccess(t *testing.T) {
	probe := newTestOracleProbe(t)
	probe.db = sql.OpenDB(&fakeOracleConnector{})
	defer probe.db.Close()

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect produced no datapoints")
	}

	assertIdentityKeys(t, points, probe.instance)

	check := mustPoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != 0 {
		t.Errorf("check result value = %v, want 0", check.Value)
	}
	if got, ok := tagValue(check.Tags, "metric_type"); !ok || got != metricTypeStatus {
		t.Errorf("check result metric_type = %q/%v, want %q", got, ok, metricTypeStatus)
	}
}

func TestOracleCollect_FailedConnectionSetsCheckResultAndIdentityFallback(t *testing.T) {
	probe := newTestOracleProbe(t)
	pingErr := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	probe.db = sql.OpenDB(&fakeOracleConnector{pingErr: pingErr})
	defer probe.db.Close()

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertIdentityKeys(t, points, probe.instance)

	up := mustPoint(t, points, "senhub.db.up")
	if up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}

	check := mustPoint(t, points, dbcommon.CheckResultMetric)
	want := float64(dbcommon.CheckTargetDown)
	if check.Value != want {
		t.Errorf("check result value = %v, want %v", check.Value, want)
	}
	if got, ok := tagValue(check.Tags, "metric_type"); !ok || got != metricTypeStatus {
		t.Errorf("check result metric_type = %q/%v, want %q", got, ok, metricTypeStatus)
	}
}
