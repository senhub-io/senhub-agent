package mysql

// families.go holds the per-`metric_type` build* functions. Each
// builder reads from the shared status / variables maps (filled
// once per Collect cycle) and appends datapoints. Replication is
// big enough to live on its own (replication.go).

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
			Name: "db_uptime_seconds", Timestamp: now, Value: v, Tags: t,
		})
	}

	// Version info — always 1, version carried as a label.
	versionTags := append([]tags.Tag{}, t...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "db_version_info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization (overview rollup; the full breakdown
	// is in the connections family).
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

// buildIOMetrics emits the `io` family — InnoDB engine-side reads
// and writes. Complements (does not replace) host-level disk stats
// from the logicaldisk probe.
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
