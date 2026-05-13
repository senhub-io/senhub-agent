package mysql

import (
	"context"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

// commonTags returns the systematic tags applied to every datapoint
// emitted by this probe instance. The metric_type tag is added per
// family by the helper that builds the family.
func (p *mysqlProbe) commonTags(family dbcommon.MetricType) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: string(family)},
		{Key: "engine", Value: "mysql"},
		{Key: "instance", Value: p.cfg.Host + ":" + strconv.Itoa(p.cfg.Port)},
		{Key: "environment", Value: string(p.environment)},
	}
}

// buildUpDatapoint emits the senhub.db.up gauge. Called both on the
// happy path (after a successful ping) and on a failed ping so the
// dashboard always sees a fresh value.
func (p *mysqlProbe) buildUpDatapoint(now time.Time, up bool) datapoint.DataPoint {
	v := float32(0)
	if up {
		v = 1
	}
	return datapoint.DataPoint{
		Name:      "db_up",
		Timestamp: now,
		Value:     v,
		Tags:      p.commonTags(dbcommon.MetricTypeOverview),
	}
}

// detectRole maps the SHOW SLAVE STATUS result + SHOW STATUS for
// Slaves_connected into the Role enum (see DESIGN §5.1). Returns
// the role on success, RoleStandalone + error on failure (callers
// log the error and use Standalone as a safe fallback).
func (p *mysqlProbe) detectRole(ctx context.Context, status map[string]string) (dbcommon.Role, error) {
	// MySQL 8.0.22+ prefers SHOW REPLICA STATUS; older versions
	// only know SHOW SLAVE STATUS. Try the newer one first; on a
	// syntax error fall back to the legacy form.
	rows, err := p.db.QueryContext(ctx, "SHOW REPLICA STATUS")
	if err != nil {
		rows, err = p.db.QueryContext(ctx, "SHOW SLAVE STATUS")
		if err != nil {
			return dbcommon.RoleStandalone, err
		}
	}
	hasReplicaStatus := rows.Next()
	rows.Close()

	if hasReplicaStatus {
		return dbcommon.RoleReplica, nil
	}

	// No replica status → primary or standalone. Inspect the bulk
	// status map for connected replicas (variable name differs
	// between MySQL 5.7, 8.0, MariaDB).
	for _, k := range []string{"Slaves_connected", "Replicas_connected"} {
		if v, ok := status[k]; ok && v != "" && v != "0" {
			return dbcommon.RolePrimary, nil
		}
	}
	return dbcommon.RoleStandalone, nil
}

// fetchGlobalStatus runs a single SHOW GLOBAL STATUS and returns
// the result as a name→value map. One bulk query per cycle is
// cheaper than N targeted queries (each `SHOW GLOBAL STATUS WHERE
// Variable_name = …` is its own round-trip), so every family
// helper reads from this map.
func (p *mysqlProbe) fetchGlobalStatus(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, "SHOW GLOBAL STATUS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, 512)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}

// fetchGlobalVariables mirrors fetchGlobalStatus for SHOW GLOBAL
// VARIABLES. Variables are configuration (max_connections,
// long_query_time, …); STATUS is observation. They live in
// different SHOW commands and the probe needs both.
func (p *mysqlProbe) fetchGlobalVariables(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, "SHOW GLOBAL VARIABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, 512)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}

// asInt parses a value from a status/variable map. Returns
// (0, false) on missing or non-numeric — both are normal across
// MySQL versions (variables come and go) so callers handle the
// false case as "metric not available right now" without warning.
func asInt(m map[string]string, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// asFloat is the float64 counterpart for variables that carry
// non-integer values (e.g. long_query_time).
func asFloat(m map[string]string, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// addCount appends a counter/gauge datapoint when the source value
// is available. Wraps sanitize.CountInt32 so PRTG never gets an
// over-range integer.
func (p *mysqlProbe) addCount(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: p.commonTags(family),
	})
}

// addRatio appends a ratio (gauge in [0,1]) when both numerator
// and denominator are non-zero. NaN and ±Inf are filtered.
func (p *mysqlProbe) addRatio(points []datapoint.DataPoint, name string, num, den int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
	if den <= 0 {
		return points
	}
	r := float32(num) / float32(den)
	if !sanitize.IsFinite(r) {
		return points
	}
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: r, Tags: p.commonTags(family),
	})
}

// Collect families ---------------------------------------------------

// buildOverviewMetrics emits the small handful of metrics that
// belong to the `overview` metric_type family (always emitted).
func (p *mysqlProbe) buildOverviewMetrics(now time.Time, status, vars map[string]string, role dbcommon.Role) []datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	var points []datapoint.DataPoint

	if uptime, ok := asInt(status, "Uptime"); ok {
		v, _ := sanitize.CountInt32(uptime)
		points = append(points, datapoint.DataPoint{
			Name: "db_uptime_seconds", Timestamp: now, Value: v, Tags: t,
		})
	}

	// Version info — always 1, version carried as a label.
	versionTags := append([]tags.Tag{}, t...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "db_version_info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization (overview rollup; the full
	// breakdown is in the connections family).
	threadsConnected, okT := asInt(status, "Threads_connected")
	maxConnections, okM := asInt(vars, "max_connections")
	if okT && okM && maxConnections > 0 {
		points = p.addRatio(points, "db_connections_utilization", threadsConnected, maxConnections, now, dbcommon.MetricTypeOverview)
	}

	// Replication role.
	roleTags := append([]tags.Tag{}, t...)
	roleTags = append(roleTags, tags.Tag{Key: "role", Value: role.String()})
	points = append(points, datapoint.DataPoint{
		Name: "db_replication_role", Timestamp: now, Value: role.RoleValue(), Tags: roleTags,
	})

	return points
}

// buildConnectionsMetrics emits the `connections` family.
func (p *mysqlProbe) buildConnectionsMetrics(now time.Time, status, vars map[string]string) []datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeConnections)
	var points []datapoint.DataPoint

	threadsRunning, okR := asInt(status, "Threads_running")
	threadsConnected, okC := asInt(status, "Threads_connected")
	if okR {
		points = p.addCount(points, "db_connections_active", threadsRunning, now, dbcommon.MetricTypeConnections)
	}
	if okR && okC && threadsConnected >= threadsRunning {
		points = p.addCount(points, "db_connections_idle", threadsConnected-threadsRunning, now, dbcommon.MetricTypeConnections)
	}
	if max, ok := asInt(vars, "max_connections"); ok {
		points = p.addCount(points, "db_connections_max", max, now, dbcommon.MetricTypeConnections)
	}
	// Aborted = clients dropping + auth failures. Both counters,
	// summed because most operators care about the aggregate.
	if abClients, okA := asInt(status, "Aborted_clients"); okA {
		if abConnects, okB := asInt(status, "Aborted_connects"); okB {
			points = p.addCount(points, "db_connections_aborted", abClients+abConnects, now, dbcommon.MetricTypeConnections)
		}
	}
	// Refused = out-of-slots events. Distinct from aborted —
	// signals capacity rather than client misbehaviour.
	if refused, ok := asInt(status, "Connection_errors_max_connections"); ok {
		points = p.addCount(points, "db_connections_refused", refused, now, dbcommon.MetricTypeConnections)
	}

	_ = t // tags returned by helpers, kept for visual symmetry
	return points
}

// buildThroughputMetrics emits the `throughput` family.
func (p *mysqlProbe) buildThroughputMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	if q, ok := asInt(status, "Questions"); ok {
		points = p.addCount(points, "db_queries_count", q, now, dbcommon.MetricTypeThroughput)
	}
	if c, ok := asInt(status, "Com_commit"); ok {
		points = p.addCount(points, "db_transactions_committed", c, now, dbcommon.MetricTypeThroughput)
	}
	if r, ok := asInt(status, "Com_rollback"); ok {
		points = p.addCount(points, "db_transactions_rolled_back", r, now, dbcommon.MetricTypeThroughput)
	}
	if s, ok := asInt(status, "Slow_queries"); ok {
		points = p.addCount(points, "db_mysql_slow_queries_count", s, now, dbcommon.MetricTypeThroughput)
	}

	// Per-command counters carry the verb as a label so PromQL
	// users can group by command without an explosion of metric
	// names. Cardinality stays bounded (a handful of well-known
	// verbs).
	for _, verb := range []string{"select", "insert", "update", "delete", "replace"} {
		if v, ok := asInt(status, "Com_"+verb); ok {
			verbTags := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypeThroughput)...)
			verbTags = append(verbTags, tags.Tag{Key: "command", Value: verb})
			val, _ := sanitize.CountInt32(v)
			points = append(points, datapoint.DataPoint{
				Name: "db_mysql_command_count", Timestamp: now, Value: val, Tags: verbTags,
			})
		}
	}

	// Tmp-tables-to-disk ratio — a tuning hint, not a SLA. Above
	// 25 % usually means tmp_table_size is too small.
	tmpDisk, okD := asInt(status, "Created_tmp_disk_tables")
	tmpTotal, okT := asInt(status, "Created_tmp_tables")
	if okD && okT {
		points = p.addRatio(points, "db_mysql_tmp_tables_disk_ratio", tmpDisk, tmpDisk+tmpTotal, now, dbcommon.MetricTypeThroughput)
	}

	return points
}

// buildReplicationMetrics emits the `replication` family. Only
// useful when role is Primary or Replica — Standalone returns
// an empty slice so dashboards don't see misleading zeros.
//
// As a side effect, returns the composite health value (0 or 1)
// so the overview family helper can expose it as
// db_replication_health (see DESIGN §5.2). Standalone reports 1
// by convention — no replication problem to detect.
func (p *mysqlProbe) buildReplicationMetrics(ctx context.Context, now time.Time, status map[string]string, role dbcommon.Role) ([]datapoint.DataPoint, float32) {
	if role == dbcommon.RoleStandalone {
		return nil, 1
	}
	t := p.commonTags(dbcommon.MetricTypeReplication)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})

	var points []datapoint.DataPoint
	health := float32(1)

	if role == dbcommon.RoleReplica {
		replicaPts, ok := p.queryReplicaStatus(ctx, now, roleTagged)
		points = append(points, replicaPts...)
		// Composite health: io_running AND sql_running AND
		// lag_seconds < threshold. Anything missing fails the
		// check rather than passing silently.
		if !ok.io || !ok.sql || ok.lagSeconds < 0 || ok.lagSeconds > float32(p.cfg.MaxReplicationLagSeconds) {
			health = 0
		}
	}

	// Replicas connected (primary or replica with downstream).
	for _, k := range []string{"Slaves_connected", "Replicas_connected"} {
		if v, ok := asInt(status, k); ok {
			val, _ := sanitize.CountInt32(v)
			points = append(points, datapoint.DataPoint{
				Name: "db_replication_replicas_connected", Timestamp: now, Value: val, Tags: roleTagged,
			})
			break
		}
	}

	return points, health
}

// replicationProbe holds the parsed signals from SHOW REPLICA
// STATUS that feed the composite health computation.
type replicationProbe struct {
	io         bool
	sql        bool
	lagSeconds float32 // -1 = unknown
}

// buildReplicationHealth emits db_replication_health under the
// overview family. The value is the composite 0/1 derived from the
// SHOW REPLICA STATUS signals (see DESIGN §5.2). Standalone gets 1
// by convention — "no replication problem to detect".
func (p *mysqlProbe) buildReplicationHealth(now time.Time, role dbcommon.Role, health float32) datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})
	return datapoint.DataPoint{
		Name: "db_replication_health", Timestamp: now, Value: health, Tags: roleTagged,
	}
}

// queryReplicaStatus runs SHOW REPLICA STATUS (or SHOW SLAVE
// STATUS on older versions), emits the IO/SQL thread state + lag
// metrics, and returns the parsed signals so the caller can fold
// them into the composite replication.health computation.
func (p *mysqlProbe) queryReplicaStatus(ctx context.Context, now time.Time, repTags []tags.Tag) ([]datapoint.DataPoint, replicationProbe) {
	probe := replicationProbe{lagSeconds: -1}
	stmt := "SHOW REPLICA STATUS"
	rows, err := p.db.QueryContext(ctx, stmt)
	if err != nil {
		stmt = "SHOW SLAVE STATUS"
		rows, err = p.db.QueryContext(ctx, stmt)
		if err != nil {
			p.logger.Warn().Err(err).Msg("replica status query failed")
			return nil, probe
		}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, probe
	}
	if !rows.Next() {
		return nil, probe
	}
	raw := make([]interface{}, len(cols))
	rawPtr := make([]interface{}, len(cols))
	for i := range raw {
		rawPtr[i] = &raw[i]
	}
	if err := rows.Scan(rawPtr...); err != nil {
		return nil, probe
	}
	values := make(map[string]string, len(cols))
	for i, name := range cols {
		values[name] = stringifyRaw(raw[i])
	}

	var points []datapoint.DataPoint
	emit := func(name string, val float32) {
		points = append(points, datapoint.DataPoint{
			Name: name, Timestamp: now, Value: val, Tags: repTags,
		})
	}

	// Thread state — Slave_IO_Running / Replica_IO_Running →
	// "Yes" / "No" string. 1 if Yes, 0 otherwise.
	for _, key := range []string{"Slave_IO_Running", "Replica_IO_Running"} {
		if v, ok := values[key]; ok {
			val := float32(0)
			if strings.EqualFold(v, "Yes") {
				val = 1
				probe.io = true
			}
			emit("db_replication_io_running", val)
			break
		}
	}
	for _, key := range []string{"Slave_SQL_Running", "Replica_SQL_Running"} {
		if v, ok := values[key]; ok {
			val := float32(0)
			if strings.EqualFold(v, "Yes") {
				val = 1
				probe.sql = true
			}
			emit("db_replication_sql_running", val)
			break
		}
	}

	// Lag in seconds.
	for _, key := range []string{"Seconds_Behind_Master", "Seconds_Behind_Source"} {
		if v, ok := values[key]; ok {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				lag := float32(n)
				if !sanitize.IsFinite(lag) {
					break
				}
				if lag < 0 {
					lag = 0
				}
				probe.lagSeconds = lag
				emit("db_replication_lag_seconds", lag)
			}
			break
		}
	}

	return points, probe
}

// buildCacheMetrics emits the `cache` family.
func (p *mysqlProbe) buildCacheMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	// InnoDB buffer pool hit ratio. The canonical computation is
	// 1 - (Innodb_buffer_pool_reads / Innodb_buffer_pool_read_requests).
	reads, okR := asInt(status, "Innodb_buffer_pool_reads")
	reqs, okQ := asInt(status, "Innodb_buffer_pool_read_requests")
	if okR && okQ && reqs > 0 {
		hits := reqs - reads
		points = p.addRatio(points, "db_buffer_hit_ratio", hits, reqs, now, dbcommon.MetricTypeCache)
	}

	// Buffer utilization — pages_data / pages_total.
	pData, okD := asInt(status, "Innodb_buffer_pool_pages_data")
	pTotal, okT := asInt(status, "Innodb_buffer_pool_pages_total")
	if okD && okT {
		points = p.addRatio(points, "db_buffer_utilization", pData, pTotal, now, dbcommon.MetricTypeCache)
	}

	// Dirty pages — pressure on the checkpointer.
	if dirty, ok := asInt(status, "Innodb_buffer_pool_pages_dirty"); ok {
		points = p.addCount(points, "db_buffer_dirty_pages", dirty, now, dbcommon.MetricTypeCache)
	}

	return points
}

// buildLocksMetrics emits the `locks` family.
func (p *mysqlProbe) buildLocksMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	if d, ok := asInt(status, "Innodb_deadlocks"); ok {
		points = p.addCount(points, "db_locks_deadlocks", d, now, dbcommon.MetricTypeLocks)
	}
	if w, ok := asInt(status, "Innodb_row_lock_current_waits"); ok {
		points = p.addCount(points, "db_locks_waiting", w, now, dbcommon.MetricTypeLocks)
	}
	if avg, ok := asInt(status, "Innodb_row_lock_time_avg"); ok {
		// Innodb_row_lock_time_avg is already in milliseconds.
		points = p.addCount(points, "db_locks_row_lock_time_avg_ms", avg, now, dbcommon.MetricTypeLocks)
	}

	return points
}

// buildIOMetrics emits the `io` family.
func (p *mysqlProbe) buildIOMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	if r, ok := asInt(status, "Innodb_data_read"); ok {
		points = p.addCount(points, "db_io_read_bytes", r, now, dbcommon.MetricTypeIO)
	}
	if w, ok := asInt(status, "Innodb_data_written"); ok {
		points = p.addCount(points, "db_io_write_bytes", w, now, dbcommon.MetricTypeIO)
	}

	return points
}

// buildStorageMetrics emits the `storage` family. Queries
// information_schema.tables once, summed across all user
// databases. Cheap on a healthy server but can take seconds on
// schemas with tens of thousands of tables — the per-cycle
// timeout protects against runaway queries.
func (p *mysqlProbe) buildStorageMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	t := p.commonTags(dbcommon.MetricTypeStorage)

	var totalBytes int64
	var tableCount int64
	row := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(data_length + index_length), 0), COALESCE(COUNT(*), 0)
		 FROM information_schema.tables
		 WHERE table_schema NOT IN ('mysql','performance_schema','information_schema','sys')`)
	if err := row.Scan(&totalBytes, &tableCount); err != nil {
		p.logger.Warn().Err(err).Msg("storage query failed (information_schema not readable)")
		return nil
	}

	if v, _ := sanitize.BytesInt32(totalBytes); true {
		points = append(points, datapoint.DataPoint{
			Name: "db_size_bytes", Timestamp: now, Value: v, Tags: t,
		})
	}
	if v, _ := sanitize.CountInt32(tableCount); true {
		points = append(points, datapoint.DataPoint{
			Name: "db_tables_count", Timestamp: now, Value: v, Tags: t,
		})
	}
	return points
}

// stringifyRaw normalises the values coming back from a
// rows.Scan(&interface{}) into a string. database/sql may return
// []byte for VARCHAR depending on the driver; SHOW SLAVE STATUS
// is full of stringly-typed columns so we coerce defensively.
func stringifyRaw(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return strconv.FormatInt(0, 10)
	}
}

// buildPerDatabaseMetrics emits one db_database_size_bytes point
// per non-system database when the operator has set
// expose_per_database. Cardinality scales with the number of
// databases; system schemas are skipped unless
// include_system_databases is also set.
func (p *mysqlProbe) buildPerDatabaseMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	if !p.cfg.ExposePerDatabase {
		return nil
	}
	q := `SELECT table_schema, COALESCE(SUM(data_length + index_length), 0)
	      FROM information_schema.tables
	      GROUP BY table_schema`
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		p.logger.Warn().Err(err).Msg("per-database query failed")
		return nil
	}
	defer rows.Close()

	var points []datapoint.DataPoint
	for rows.Next() {
		var dbName string
		var sizeBytes int64
		if err := rows.Scan(&dbName, &sizeBytes); err != nil {
			continue
		}
		if !p.cfg.IncludeSystemDatabases && dbcommon.IsSystemDatabase(dbName) {
			continue
		}
		tagsRow := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypePerDatabase)...)
		tagsRow = append(tagsRow, tags.Tag{Key: "database", Value: dbName})
		v, _ := sanitize.BytesInt32(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "db_database_size_bytes", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}

// buildPerTableMetrics emits db_table_size_bytes for the top-N
// largest user tables when expose_top_tables > 0. The cap is
// enforced at the SQL layer (ORDER BY size DESC LIMIT N) so the
// agent never streams every row of information_schema.tables for
// a database with thousands of tables.
func (p *mysqlProbe) buildPerTableMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	if p.cfg.ExposeTopTables <= 0 {
		return nil
	}
	q := `SELECT table_schema, table_name, (data_length + index_length) AS size_bytes
	      FROM information_schema.tables
	      WHERE table_schema NOT IN ('mysql','performance_schema','information_schema','sys')
	        AND data_length IS NOT NULL
	      ORDER BY size_bytes DESC
	      LIMIT ?`
	rows, err := p.db.QueryContext(ctx, q, p.cfg.ExposeTopTables)
	if err != nil {
		p.logger.Warn().Err(err).Msg("per-table query failed")
		return nil
	}
	defer rows.Close()

	var points []datapoint.DataPoint
	for rows.Next() {
		var schema, table string
		var sizeBytes int64
		if err := rows.Scan(&schema, &table, &sizeBytes); err != nil {
			continue
		}
		tagsRow := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypePerTable)...)
		tagsRow = append(tagsRow,
			tags.Tag{Key: "schema", Value: schema},
			tags.Tag{Key: "table", Value: table},
		)
		v, _ := sanitize.BytesInt32(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "db_table_size_bytes", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}
