package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

func newMSSQLQueriesLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

type cannedDriver struct {
	pingErr  error
	queryErr map[string]error
	rows     map[string]*cannedRows
}

func (d *cannedDriver) Open(name string) (driver.Conn, error) {
	return &cannedConn{driver: d}, nil
}

type cannedConnector struct {
	driver *cannedDriver
}

func (c *cannedConnector) Connect(context.Context) (driver.Conn, error) {
	return &cannedConn{driver: c.driver}, nil
}

func (c *cannedConnector) Driver() driver.Driver {
	return c.driver
}

type cannedConn struct {
	driver *cannedDriver
}

func (c *cannedConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("fake driver does not implement Prepare")
}

func (c *cannedConn) Close() error { return nil }

func (c *cannedConn) Begin() (driver.Tx, error) {
	return nil, errors.New("fake driver does not implement Begin")
}

func (c *cannedConn) Ping(ctx context.Context) error { return c.driver.pingErr }

func (c *cannedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.driver.queryErr[query]; err != nil {
		return nil, err
	}
	rows, ok := c.driver.rows[query]
	if !ok {
		return nil, errors.New("fake driver: unexpected query")
	}
	return rows, nil
}

type cannedRows struct {
	columns []string
	rows    [][]driver.Value
	step    int
	errAt   int
}

func newFakeRows(columns []string, rows [][]driver.Value) *cannedRows {
	return &cannedRows{columns: columns, rows: rows, errAt: -1}
}

func (r *cannedRows) Columns() []string { return r.columns }
func (r *cannedRows) Close() error      { return nil }

func (r *cannedRows) Next(dest []driver.Value) error {
	if r.errAt >= 0 && r.step == r.errAt {
		r.step++
		return errors.New("mid-iteration failure")
	}
	if r.step < len(r.rows) {
		copy(dest, r.rows[r.step])
		r.step++
		return nil
	}
	return io.EOF
}

func newFakeConnector(pingErr error, queryErr map[string]error, rows map[string]*cannedRows) *cannedConnector {
	return &cannedConnector{driver: &cannedDriver{pingErr: pingErr, queryErr: queryErr, rows: rows}}
}

func newMSSQLQueriesProbe(t *testing.T, raw map[string]interface{}) *MSSQLProbe {
	t.Helper()
	probe, err := NewMSSQLProbe(raw, newMSSQLQueriesLogger())
	if err != nil {
		t.Fatalf("NewMSSQLProbe: %v", err)
	}
	return probe.(*MSSQLProbe)
}

func newMSSQLQueriesCollectProbe(t *testing.T, connector *cannedConnector) *MSSQLProbe {
	t.Helper()
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1", "port": 1433})
	p.SetName("mssql-test")
	p.db = sql.OpenDB(connector)
	t.Cleanup(func() { _ = p.db.Close() })
	return p
}

func queryPointsByName(points []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, dp := range points {
		if dp.Name == name {
			out = append(out, dp)
		}
	}
	return out
}

func queryRequirePoint(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	matches := queryPointsByName(points, name)
	if len(matches) == 0 {
		t.Fatalf("metric %s not found", name)
	}
	if len(matches) > 1 {
		t.Fatalf("metric %s emitted %d times, want exactly one", name, len(matches))
	}
	return matches[0]
}

func queryHasPoint(points []data_store.DataPoint, name string) bool {
	return len(queryPointsByName(points, name)) > 0
}

func queryTags(t *testing.T, dp data_store.DataPoint) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, tag := range dp.Tags {
		if _, exists := got[tag.Key]; exists {
			t.Fatalf("%s has duplicate tag %s", dp.Name, tag.Key)
		}
		got[tag.Key] = tag.Value
	}
	return got
}

func queryAssertBaseTags(t *testing.T, dp data_store.DataPoint, wantMetricType string) {
	t.Helper()
	got := queryTags(t, dp)
	if got["metric_type"] != wantMetricType {
		t.Errorf("%s metric_type = %q, want %q", dp.Name, got["metric_type"], wantMetricType)
	}
	if got["instance"] != "db1:1433" {
		t.Errorf("%s instance = %q, want db1:1433", dp.Name, got["instance"])
	}
	if got["db.system.name"] != dbSystemMSSQL {
		t.Errorf("%s db.system.name = %q, want %q", dp.Name, got["db.system.name"], dbSystemMSSQL)
	}
	if got["server.address"] != "db1" {
		t.Errorf("%s server.address = %q, want db1", dp.Name, got["server.address"])
	}
	if got["server.port"] != "1433" {
		t.Errorf("%s server.port = %q, want 1433", dp.Name, got["server.port"])
	}
	if got["db.instance.id"] == "" {
		t.Errorf("%s has empty db.instance.id", dp.Name)
	}
}

func queryAssertCollectTags(t *testing.T, dp data_store.DataPoint, wantMetricType string) {
	t.Helper()
	queryAssertBaseTags(t, dp, wantMetricType)
	got := queryTags(t, dp)
	if got["probe_name"] != "mssql-test" {
		t.Errorf("%s probe_name = %q, want mssql-test", dp.Name, got["probe_name"])
	}
}

func TestCollect_NilDBEmitsDownAndProtocolCheckResult(t *testing.T) {
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1"})
	p.SetName("mssql-test")
	p.db = nil

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("Collect returned %d points, want exactly 2", len(points))
	}

	up := queryRequirePoint(t, points, "senhub.db.up")
	if up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}
	queryAssertCollectTags(t, up, metricTypeOverview)

	check := queryRequirePoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != float64(dbcommon.CheckProtocolError) {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckProtocolError))
	}
	queryAssertCollectTags(t, check, metricTypeStatus)
}

func TestCollect_PingFailureEmitsOnlyDownMetricsAndClassifiesTargetDown(t *testing.T) {
	conn := newFakeConnector(errors.New("dial tcp 10.0.0.5:1433: connect: connection refused"), nil, nil)
	p := newMSSQLQueriesCollectProbe(t, conn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("Collect returned %d points, want exactly 2", len(points))
	}

	up := queryRequirePoint(t, points, "senhub.db.up")
	if up.Value != 0 {
		t.Errorf("senhub.db.up = %v, want 0", up.Value)
	}
	queryAssertCollectTags(t, up, metricTypeOverview)

	check := queryRequirePoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckTargetDown))
	}
	queryAssertCollectTags(t, check, metricTypeStatus)
}

func TestCollect_HealthyEmitsAllPerformanceCountersWithMetricTypeTags(t *testing.T) {
	perfRows := newFakeRows([]string{"counter_name", "instance_name", "cntr_value"}, [][]driver.Value{
		{"Batch Requests/sec", "", float64(100)},
		{"Transactions/sec", "_Total", float64(20)},
		{"Lock Waits/sec", "", float64(5)},
		{"Processes blocked", "", float64(2)},
		{"User Connections", "", float64(30)},
		{"Page life expectancy", "", float64(45)},
		{"Buffer cache hit ratio", "", float64(82)},
		{"Buffer cache hit ratio base", "", float64(100)},
	})
	ioRows := newFakeRows([]string{"database_name", "bytes_read", "bytes_written"}, nil)
	statusRows := newFakeRows([]string{"name", "state"}, nil)

	conn := newFakeConnector(nil, nil, map[string]*cannedRows{
		performanceCountersQuery: perfRows,
		databaseIOQuery:          ioRows,
		databaseStatusQuery:      statusRows,
	})
	p := newMSSQLQueriesCollectProbe(t, conn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) != 9 {
		t.Fatalf("Collect returned %d points, want 9 (up, check and 7 perf counters)", len(points))
	}

	up := queryRequirePoint(t, points, "senhub.db.up")
	if up.Value != 1 {
		t.Errorf("senhub.db.up = %v, want 1", up.Value)
	}
	queryAssertCollectTags(t, up, metricTypeOverview)

	check := queryRequirePoint(t, points, dbcommon.CheckResultMetric)
	if check.Value != float64(dbcommon.CheckOK) {
		t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckOK))
	}
	queryAssertCollectTags(t, check, metricTypeStatus)

	want := map[string]float64{
		"sqlserver.batch_request.rate":          100,
		"sqlserver.transaction_rate":            20,
		"sqlserver.lock_wait_rate":              5,
		"sqlserver.processes.blocked":           2,
		"sqlserver.user.connection.count":       30,
		"sqlserver.page_life_expectancy":        45,
		"sqlserver.page_buffer_cache.hit_ratio": 82,
	}
	wantTypes := map[string]string{
		"sqlserver.batch_request.rate":          metricTypeThroughput,
		"sqlserver.transaction_rate":            metricTypeThroughput,
		"sqlserver.lock_wait_rate":              metricTypeLocks,
		"sqlserver.processes.blocked":           metricTypeLocks,
		"sqlserver.user.connection.count":       metricTypeConnections,
		"sqlserver.page_life_expectancy":        metricTypeCache,
		"sqlserver.page_buffer_cache.hit_ratio": metricTypeCache,
	}

	for name, wantValue := range want {
		dp := queryRequirePoint(t, points, name)
		if dp.Value != wantValue {
			t.Errorf("%s = %v, want %v", name, dp.Value, wantValue)
		}
		queryAssertCollectTags(t, dp, wantTypes[name])
	}
}

func TestCollectPerformanceCounters_MissingCounterAndZeroBaseOmitRatio(t *testing.T) {
	cases := []struct {
		name    string
		rows    [][]driver.Value
		missing []string
	}{
		{
			name:    "regular mapped counter missing",
			rows:    [][]driver.Value{{"Batch Requests/sec", "", float64(9)}},
			missing: []string{"sqlserver.transaction_rate"},
		},
		{
			name: "cache base zero",
			rows: [][]driver.Value{
				{"Buffer cache hit ratio", "", float64(80)},
				{"Buffer cache hit ratio base", "", float64(0)},
			},
			missing: []string{"sqlserver.page_buffer_cache.hit_ratio"},
		},
		{
			name: "cache base missing",
			rows: [][]driver.Value{
				{"Buffer cache hit ratio", "", float64(80)},
			},
			missing: []string{"sqlserver.page_buffer_cache.hit_ratio"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newFakeConnector(nil, nil, map[string]*cannedRows{
				performanceCountersQuery: newFakeRows([]string{"counter_name", "instance_name", "cntr_value"}, tc.rows),
			})
			p := newMSSQLQueriesCollectProbe(t, conn)

			points := p.collectPerformanceCounters(context.Background(), time.Now())
			for _, missing := range tc.missing {
				if queryHasPoint(points, missing) {
					t.Errorf("metric %s emitted but should be omitted", missing)
				}
			}
		})
	}
}

func TestCollectDatabaseIO_EmitsReadAndWriteForValidDatabasesAndSkipsInvalidRows(t *testing.T) {
	rows := newFakeRows([]string{"database_name", "bytes_read", "bytes_written"}, [][]driver.Value{
		{"AdventureWorks", int64(100), int64(200)},
		{nil, int64(999), int64(999)},
		{"", int64(300), int64(400)},
		{"ScanFail", int64(700), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"master", int64(500), int64(600)},
	})
	conn := newFakeConnector(nil, nil, map[string]*cannedRows{databaseIOQuery: rows})
	p := newMSSQLQueriesCollectProbe(t, conn)

	points := p.collectDatabaseIO(context.Background(), time.Now())
	if len(points) != 4 {
		t.Fatalf("collectDatabaseIO returned %d points, want 4 read/write for two valid databases", len(points))
	}

	want := map[string]float64{
		"AdventureWorks:read":  100,
		"AdventureWorks:write": 200,
		"master:read":          500,
		"master:write":         600,
	}
	for _, dp := range points {
		got := queryTags(t, dp)
		key := got["database"] + ":" + got["direction"]
		wantValue, ok := want[key]
		if !ok {
			t.Errorf("unexpected database.io point for %q", key)
			continue
		}
		if dp.Value != wantValue {
			t.Errorf("database.io %s = %v, want %v", key, dp.Value, wantValue)
		}
		queryAssertBaseTags(t, dp, metricTypeIO)
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing database.io points for keys %v", want)
	}
}

func TestCollectDatabaseStatus_TrimsNamesPassesStateAndSkipsBlankAndScanErrors(t *testing.T) {
	rows := newFakeRows([]string{"name", "state"}, [][]driver.Value{
		{"  master  ", int64(0)},
		{"tempdb", int64(6)},
		{"", int64(5)},
		{"bad", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	conn := newFakeConnector(nil, nil, map[string]*cannedRows{databaseStatusQuery: rows})
	p := newMSSQLQueriesCollectProbe(t, conn)

	points := p.collectDatabaseStatus(context.Background(), time.Now())
	if len(points) != 2 {
		t.Fatalf("collectDatabaseStatus returned %d points, want 2 valid status points", len(points))
	}

	want := map[string]float64{"master": 0, "tempdb": 6}
	for _, dp := range points {
		got := queryTags(t, dp)
		database := got["database"]
		wantState, ok := want[database]
		if !ok {
			t.Errorf("unexpected database.status point for %q", database)
			continue
		}
		if dp.Value != wantState {
			t.Errorf("database.status %s = %v, want %v", database, dp.Value, wantState)
		}
		queryAssertBaseTags(t, dp, metricTypeStorage)
		delete(want, database)
	}
	if len(want) != 0 {
		t.Errorf("missing database.status points for databases %v", want)
	}
}

func TestCollect_QueryFailureSilencesOnlyFailingFamily(t *testing.T) {
	cases := []struct {
		name         string
		failingQuery string
		absent       []string
		present      []string
	}{
		{
			name:         "performance counters",
			failingQuery: performanceCountersQuery,
			absent: []string{
				"sqlserver.batch_request.rate",
				"sqlserver.transaction_rate",
				"sqlserver.lock_wait_rate",
				"sqlserver.processes.blocked",
				"sqlserver.user.connection.count",
				"sqlserver.page_life_expectancy",
				"sqlserver.page_buffer_cache.hit_ratio",
			},
			present: []string{"sqlserver.database.io", "sqlserver.database.status"},
		},
		{
			name:         "database io",
			failingQuery: databaseIOQuery,
			absent:       []string{"sqlserver.database.io"},
			present:      []string{"sqlserver.batch_request.rate", "sqlserver.database.status"},
		},
		{
			name:         "database status",
			failingQuery: databaseStatusQuery,
			absent:       []string{"sqlserver.database.status"},
			present:      []string{"sqlserver.batch_request.rate", "sqlserver.database.io"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			perfRows := newFakeRows([]string{"counter_name", "instance_name", "cntr_value"}, [][]driver.Value{
				{"Batch Requests/sec", "", float64(1)},
			})
			ioRows := newFakeRows([]string{"database_name", "bytes_read", "bytes_written"}, [][]driver.Value{
				{"master", int64(1), int64(2)},
			})
			statusRows := newFakeRows([]string{"name", "state"}, [][]driver.Value{
				{"master", int64(0)},
			})

			conn := newFakeConnector(nil, map[string]error{tc.failingQuery: errors.New("query failed")}, map[string]*cannedRows{
				performanceCountersQuery: perfRows,
				databaseIOQuery:          ioRows,
				databaseStatusQuery:      statusRows,
			})
			p := newMSSQLQueriesCollectProbe(t, conn)

			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			for _, name := range tc.absent {
				if queryHasPoint(points, name) {
					t.Errorf("metric %s emitted although %s failed", name, tc.name)
				}
			}
			for _, name := range tc.present {
				if !queryHasPoint(points, name) {
					t.Errorf("metric %s missing after %s failed", name, tc.name)
				}
			}

			up := queryRequirePoint(t, points, "senhub.db.up")
			if up.Value != 1 {
				t.Errorf("senhub.db.up = %v, want 1", up.Value)
			}
			check := queryRequirePoint(t, points, dbcommon.CheckResultMetric)
			if check.Value != float64(dbcommon.CheckOK) {
				t.Errorf("%s = %v, want %v", dbcommon.CheckResultMetric, check.Value, float64(dbcommon.CheckOK))
			}
		})
	}
}

func TestCollect_EveryDatapointCarriesProbeNameAndOTelResourceTags(t *testing.T) {
	perfRows := newFakeRows([]string{"counter_name", "instance_name", "cntr_value"}, [][]driver.Value{
		{"Batch Requests/sec", "", float64(1)},
		{"Buffer cache hit ratio", "", float64(80)},
		{"Buffer cache hit ratio base", "", float64(100)},
	})
	ioRows := newFakeRows([]string{"database_name", "bytes_read", "bytes_written"}, [][]driver.Value{
		{"master", int64(1), int64(2)},
	})
	statusRows := newFakeRows([]string{"name", "state"}, [][]driver.Value{
		{"master", int64(0)},
	})

	conn := newFakeConnector(nil, nil, map[string]*cannedRows{
		performanceCountersQuery: perfRows,
		databaseIOQuery:          ioRows,
		databaseStatusQuery:      statusRows,
	})
	p := newMSSQLQueriesCollectProbe(t, conn)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) < 3 {
		t.Fatalf("Collect returned only %d points, want at least 3", len(points))
	}

	for _, dp := range points {
		got := queryTags(t, dp)
		if got["probe_name"] != "mssql-test" {
			t.Errorf("%s probe_name = %q, want mssql-test", dp.Name, got["probe_name"])
		}
		if got["instance"] != "db1:1433" {
			t.Errorf("%s instance = %q, want db1:1433", dp.Name, got["instance"])
		}
		if got["db.system.name"] != dbSystemMSSQL {
			t.Errorf("%s db.system.name = %q, want %q", dp.Name, got["db.system.name"], dbSystemMSSQL)
		}
		if got["server.address"] != "db1" {
			t.Errorf("%s server.address = %q, want db1", dp.Name, got["server.address"])
		}
		if got["server.port"] != "1433" {
			t.Errorf("%s server.port = %q, want 1433", dp.Name, got["server.port"])
		}
		if got["db.instance.id"] == "" {
			t.Errorf("%s has empty db.instance.id", dp.Name)
		}
		if got["metric_type"] == "" {
			t.Errorf("%s has empty metric_type", dp.Name)
		}
	}
}

func TestCollectDatabaseStatus_RowsIterationErrorKeepsAlreadyCollectedPoint(t *testing.T) {
	rows := newFakeRows([]string{"name", "state"}, [][]driver.Value{{"master", int64(0)}})
	rows.errAt = 1
	conn := newFakeConnector(nil, nil, map[string]*cannedRows{databaseStatusQuery: rows})
	p := newMSSQLQueriesCollectProbe(t, conn)

	points := p.collectDatabaseStatus(context.Background(), time.Now())
	if len(points) != 1 {
		t.Fatalf("collectDatabaseStatus returned %d points, want 1 point collected before rows error", len(points))
	}
	if points[0].Value != 0 {
		t.Errorf("database.status master = %v, want 0", points[0].Value)
	}
}

func TestOnStart_OpensConnectionPoolWithoutConnecting(t *testing.T) {
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1"})
	t.Cleanup(func() {
		if p.db != nil {
			_ = p.db.Close()
		}
	})

	if err := p.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if p.db == nil {
		t.Fatal("OnStart did not set p.db")
	}

	if err := p.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
}

func TestOnShutdown_NilDBIsNoop(t *testing.T) {
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1"})
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown with nil db: %v", err)
	}
}

func TestMSSQLProbeLifecycleAccessors(t *testing.T) {
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1", "interval": 90})

	if !p.ShouldStart() {
		t.Error("ShouldStart = false, want true")
	}

	wantStrategies := []string{"senhub", "prtg", "http", "otlp"}
	if got := p.GetTargetStrategies(); !reflect.DeepEqual(got, wantStrategies) {
		t.Errorf("GetTargetStrategies = %v, want %v", got, wantStrategies)
	}

	if got := p.GetInterval(); got != 90*time.Second {
		t.Errorf("GetInterval = %v, want 90s", got)
	}
}

func TestTaggedWith_DoesNotCorruptBaseTagsAcrossCalls(t *testing.T) {
	p := newMSSQLQueriesProbe(t, map[string]interface{}{"host": "db1"})
	p.SetName("mssql-test")

	before := p.baseTags(metricTypeOverview)
	if len(before) != 6 {
		t.Fatalf("baseTags returned %d tags, want 6", len(before))
	}

	first := p.taggedWith(metricTypeIO, tags.Tag{Key: "database", Value: "AdventureWorks"})
	second := p.taggedWith(metricTypeStorage, tags.Tag{Key: "direction", Value: "read"})

	if first[0].Key != "metric_type" || first[0].Value != metricTypeIO {
		t.Errorf("first taggedWith metric_type = %s=%q, want %q", first[0].Key, first[0].Value, metricTypeIO)
	}
	if second[0].Key != "metric_type" || second[0].Value != metricTypeStorage {
		t.Errorf("second taggedWith metric_type = %s=%q, want %q", second[0].Key, second[0].Value, metricTypeStorage)
	}
	for _, tag := range second {
		if tag.Key == "database" {
			t.Error("second taggedWith carries database tag from first call")
		}
	}

	after := p.baseTags(metricTypeOverview)
	if len(after) != len(before) {
		t.Fatalf("baseTags returned %d tags after taggedWith calls, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].Key != before[i].Key || after[i].Value != before[i].Value {
			t.Errorf("baseTags changed at index %d: got %s=%q, want %s=%q", i, after[i].Key, after[i].Value, before[i].Key, before[i].Value)
		}
	}
}
