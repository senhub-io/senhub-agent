package mysql

// families.go holds the per-`metric_type` build* functions. Each
// builder reads from the shared status / variables maps (filled
// once per Collect cycle) and appends datapoints. Replication is
// big enough to live on its own (replication.go).
//
// Metric naming follows the OTel-first conventions defined in
// docs/developer-guide/otel/senhub-semantic-conventions.md §4.13.
// Internal probe metric names match the YAML transformer entries
// (mysql.*, senhub.db.*, senhub.db.mysql.*) — see mysql.yaml for
// the canonical list and OTel mapping.

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

// buildOverviewMetrics emits the small handful of metrics that
// belong to the `overview` metric_type family (always emitted).
func (p *mysqlProbe) buildOverviewMetrics(now time.Time, status, vars map[string]string, role dbcommon.Role) []datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	var points []datapoint.DataPoint

	if uptime, ok := asInt(status, "Uptime"); ok {
		v, _ := sanitize.CountInt32(uptime)
		points = append(points, datapoint.DataPoint{
			Name: "mysql.uptime", Timestamp: now, Value: v, Tags: t,
		})
	}

	// Version info — always 1, version carried as a label that the
	// YAML's tag_to_attribute lifts into db.system.version on OTLP.
	versionTags := append([]tags.Tag{}, t...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "senhub.db.version.info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization (overview rollup; the full breakdown
	// is in the connections family).
	threadsConnected, okT := asInt(status, "Threads_connected")
	maxConnections, okM := asInt(vars, "max_connections")
	if okT && okM && maxConnections > 0 {
		points = p.addRatio(points, "senhub.db.connection.utilization", threadsConnected, maxConnections, now, dbcommon.MetricTypeOverview)
	}

	// Replication role (raw enum value — the OTel mapper expands it
	// into N per-state datapoints at serialization time via
	// otel.expand).
	roleTags := append([]tags.Tag{}, t...)
	roleTags = append(roleTags, tags.Tag{Key: "role", Value: role.String()})
	points = append(points, datapoint.DataPoint{
		Name: "senhub.db.replication.role", Timestamp: now, Value: role.RoleValue(), Tags: roleTags,
	})

	return points
}

// buildConnectionsMetrics emits the `connections` family.
//
// Naming reflects the OTel contrib mysqlreceiver `mysql.threads`
// collapse: we emit kind=connected (raw Threads_connected) and
// kind=running (raw Threads_running). The derived `senhub.db.connection.idle`
// is the clamped subtraction — emitted explicitly so PRTG / Nagios
// don't have to do arithmetic. `mysql.connection.errors` is split
// into three counters, one per cause (aborted_clients / aborted_connects
// / max_connections) for fidelity vs SHOW GLOBAL STATUS.
func (p *mysqlProbe) buildConnectionsMetrics(now time.Time, status, vars map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	threadsRunning, okR := asInt(status, "Threads_running")
	threadsConnected, okC := asInt(status, "Threads_connected")
	if okR {
		points = p.addCountTagged(points, "mysql.threads.running", threadsRunning, now, dbcommon.MetricTypeConnections, "kind", "running")
	}
	if okC {
		points = p.addCountTagged(points, "mysql.threads.connected", threadsConnected, now, dbcommon.MetricTypeConnections, "kind", "connected")
	}
	// Derived idle. Clamped to 0 because Threads_running includes
	// InnoDB background threads on MariaDB and can exceed
	// Threads_connected — without the clamp we'd publish a negative
	// "idle" count that confuses dashboards.
	if okR && okC {
		idle := threadsConnected - threadsRunning
		if idle < 0 {
			idle = 0
		}
		points = p.addCount(points, "senhub.db.connection.idle", idle, now, dbcommon.MetricTypeConnections)
	}
	if max, ok := asInt(vars, "max_connections"); ok {
		points = p.addCount(points, "senhub.db.mysql.connection.max", max, now, dbcommon.MetricTypeConnections)
	}
	// Aborted clients and aborted connects are reported as two
	// separate counters with the `error` attribute = aborted_clients
	// or aborted_connects (canon mysql.connection.errors). Refused
	// (cap-exhaustion) is the third variant.
	if abClients, ok := asInt(status, "Aborted_clients"); ok {
		points = p.addCountTagged(points, "mysql.connection.errors.aborted_clients", abClients, now, dbcommon.MetricTypeConnections, "error", "aborted_clients")
	}
	if abConnects, ok := asInt(status, "Aborted_connects"); ok {
		points = p.addCountTagged(points, "mysql.connection.errors.aborted_connects", abConnects, now, dbcommon.MetricTypeConnections, "error", "aborted_connects")
	}
	if refused, ok := asInt(status, "Connection_errors_max_connections"); ok {
		points = p.addCountTagged(points, "mysql.connection.errors.max_connections", refused, now, dbcommon.MetricTypeConnections, "error", "max_connections")
	}

	return points
}

// buildThroughputMetrics emits the `throughput` family.
func (p *mysqlProbe) buildThroughputMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	if q, ok := asInt(status, "Questions"); ok {
		points = p.addCount(points, "mysql.query.count", q, now, dbcommon.MetricTypeThroughput)
	}
	if c, ok := asInt(status, "Com_commit"); ok {
		points = p.addCountTagged(points, "senhub.db.mysql.transaction.count.committed", c, now, dbcommon.MetricTypeThroughput, "state", "committed")
	}
	if r, ok := asInt(status, "Com_rollback"); ok {
		points = p.addCountTagged(points, "senhub.db.mysql.transaction.count.rolled_back", r, now, dbcommon.MetricTypeThroughput, "state", "rolled_back")
	}
	if s, ok := asInt(status, "Slow_queries"); ok {
		points = p.addCount(points, "mysql.query.slow.count", s, now, dbcommon.MetricTypeThroughput)
	}

	// Per-command counters carry the verb as a label so PromQL
	// users can group by command without an explosion of metric
	// names. Cardinality stays bounded (a handful of well-known
	// verbs). The YAML maps tag `command` → OTel attr `command`.
	for _, verb := range []string{"select", "insert", "update", "delete", "replace"} {
		if v, ok := asInt(status, "Com_"+verb); ok {
			points = p.addCountTagged(points, "mysql.commands", v, now, dbcommon.MetricTypeThroughput, "command", verb)
		}
	}

	// Tmp-tables-to-disk ratio — a tuning hint, not a SLA. Above
	// 25 % usually means tmp_table_size is too small.
	//
	// Denominator semantics: MySQL 8.0 and MariaDB 10.x both define
	// `Created_tmp_tables` as the total of internal temp tables
	// created (in-memory + on-disk). The disk subset is reported
	// separately as `Created_tmp_disk_tables`. So the right ratio is
	// disk / total, not disk / (disk + total) which would double-count
	// the disk-spilled subset and inflate the ratio by ~2x.
	//
	// `addRatio` clamps to [0, 1] which absorbs the edge case where a
	// server somehow reports `tmpDisk > tmpTotal` (unexpected on any
	// supported version but safe by construction).
	tmpDisk, okD := asInt(status, "Created_tmp_disk_tables")
	tmpTotal, okT := asInt(status, "Created_tmp_tables")
	if okD && okT {
		points = p.addRatio(points, "senhub.db.mysql.tmp_tables.disk_ratio", tmpDisk, tmpTotal, now, dbcommon.MetricTypeThroughput)
	}

	return points
}

// buildCacheMetrics emits the `cache` family — InnoDB buffer pool
// hit ratio + utilization + dirty pages.
func (p *mysqlProbe) buildCacheMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	// InnoDB buffer pool hit ratio.
	// 1 - (Innodb_buffer_pool_reads / Innodb_buffer_pool_read_requests).
	reads, okR := asInt(status, "Innodb_buffer_pool_reads")
	reqs, okQ := asInt(status, "Innodb_buffer_pool_read_requests")
	if okR && okQ && reqs > 0 {
		hits := reqs - reads
		points = p.addRatio(points, "senhub.db.mysql.buffer_pool.hit_ratio", hits, reqs, now, dbcommon.MetricTypeCache)
	}
	// Buffer utilization — pages_data / pages_total.
	pData, okD := asInt(status, "Innodb_buffer_pool_pages_data")
	pTotal, okT := asInt(status, "Innodb_buffer_pool_pages_total")
	if okD && okT {
		points = p.addRatio(points, "senhub.db.mysql.buffer_pool.utilization", pData, pTotal, now, dbcommon.MetricTypeCache)
	}
	// Dirty pages — pressure on the checkpointer. OTel canon:
	// mysql.buffer_pool.data_pages{status=dirty}.
	if dirty, ok := asInt(status, "Innodb_buffer_pool_pages_dirty"); ok {
		points = p.addCountTagged(points, "mysql.buffer_pool.data_pages.dirty", dirty, now, dbcommon.MetricTypeCache, "status", "dirty")
	}

	return points
}

// buildLocksMetrics emits the `locks` family.
func (p *mysqlProbe) buildLocksMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	if d, ok := asInt(status, "Innodb_deadlocks"); ok {
		points = p.addCount(points, "senhub.db.mysql.lock.deadlocks", d, now, dbcommon.MetricTypeLocks)
	}
	if w, ok := asInt(status, "Innodb_row_lock_current_waits"); ok {
		points = p.addCount(points, "senhub.db.mysql.lock.waiting", w, now, dbcommon.MetricTypeLocks)
	}
	if avgMs, ok := asInt(status, "Innodb_row_lock_time_avg"); ok {
		// Source is milliseconds; OTel canonical unit for durations
		// is seconds. Convert ÷1000 here so OTLP/Prom see the right
		// unit semantics. PRTG/Nagios display still works through the
		// YAML `unit: "ms"` field, which is purely presentational.
		seconds := float32(avgMs) / 1000.0
		if !sanitize.IsFinite(seconds) {
			seconds = 0
		}
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.mysql.row_lock.time.avg", Timestamp: now, Value: seconds, Tags: p.commonTags(dbcommon.MetricTypeLocks),
		})
	}

	return points
}

// buildIOMetrics emits the `io` family — InnoDB engine-side reads
// and writes. Two datapoints under a single OTel metric name
// (senhub.db.mysql.io) discriminated by attribute io.direction.
func (p *mysqlProbe) buildIOMetrics(now time.Time, status map[string]string) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	if r, ok := asInt(status, "Innodb_data_read"); ok {
		points = p.addCountTagged(points, "senhub.db.mysql.io.read", r, now, dbcommon.MetricTypeIO, "io.direction", "read")
	}
	if w, ok := asInt(status, "Innodb_data_written"); ok {
		points = p.addCountTagged(points, "senhub.db.mysql.io.write", w, now, dbcommon.MetricTypeIO, "io.direction", "write")
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

	if v, _ := sanitize.Bytes(totalBytes); true {
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.database.size", Timestamp: now, Value: v, Tags: t,
		})
	}
	if v, _ := sanitize.CountInt32(tableCount); true {
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.mysql.table.count", Timestamp: now, Value: v, Tags: t,
		})
	}
	return points
}

// buildPerDatabaseMetrics emits one senhub.db.database.size point
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
		v, _ := sanitize.Bytes(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.database.size.per_database", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	if err := rows.Err(); err != nil {
		p.logger.Warn().Err(err).Msg("per-database row scan interrupted; partial results may flow this cycle")
	}
	return points
}

// buildPerTableMetrics emits senhub.db.mysql.table.size for the top-N
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
			tags.Tag{Key: "database", Value: schema},
			tags.Tag{Key: "table", Value: table},
		)
		v, _ := sanitize.Bytes(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.mysql.table.size", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	if err := rows.Err(); err != nil {
		p.logger.Warn().Err(err).Msg("per-table row scan interrupted; partial results may flow this cycle")
	}
	return points
}
