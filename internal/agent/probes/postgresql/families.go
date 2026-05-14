package postgresql

// families.go holds the per-metric_type builders for everything
// except the replication family (replication.go) and the SenHub
// differentiators bloat / pg_stat_statements (differentiators.go).

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

// buildOverviewMetrics emits db_uptime_seconds, db_version_info,
// db_connections_utilization, db_replication_role.
func (p *postgresqlProbe) buildOverviewMetrics(ctx context.Context, now time.Time, role dbcommon.Role) []datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	var points []datapoint.DataPoint

	// Uptime via pg_postmaster_start_time().
	var uptimeSeconds float64
	if err := p.db.QueryRowContext(ctx,
		"SELECT EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))",
	).Scan(&uptimeSeconds); err == nil {
		v, _ := sanitize.CountInt32(int64(uptimeSeconds))
		points = append(points, datapoint.DataPoint{
			Name: "db_uptime_seconds", Timestamp: now, Value: v, Tags: t,
		})
	}

	// Version info — value 1, version string as label.
	versionTags := append([]tags.Tag{}, t...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "db_version_info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization — count(pg_stat_activity) /
	// max_connections.
	var active, maxConn int64
	_ = p.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity").Scan(&active)
	_ = p.db.QueryRowContext(ctx, "SHOW max_connections").Scan(&maxConn)
	if active > 0 && maxConn > 0 {
		points = p.addRatio(points, "db_connections_utilization", active, maxConn, now, dbcommon.MetricTypeOverview)
	}

	// Replication role.
	roleTags := append([]tags.Tag{}, t...)
	roleTags = append(roleTags, tags.Tag{Key: "role", Value: role.String()})
	points = append(points, datapoint.DataPoint{
		Name: "db_replication_role", Timestamp: now, Value: role.RoleValue(), Tags: roleTags,
	})

	return points
}

// buildConnectionsMetrics emits the breakdown by state. PG-specific
// idle_in_transaction is a first-class metric (DESIGN §5.7).
func (p *postgresqlProbe) buildConnectionsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	// One query, grouped by state, gives the full breakdown.
	rows, err := p.db.QueryContext(ctx,
		"SELECT state, count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() GROUP BY state")
	if err == nil {
		defer rows.Close()
		counts := map[string]int64{}
		for rows.Next() {
			var state *string
			var n int64
			if err := rows.Scan(&state, &n); err != nil {
				continue
			}
			s := ""
			if state != nil {
				s = *state
			}
			counts[s] = n
		}
		points = p.addCount(points, "db_connections_active", counts["active"], now, dbcommon.MetricTypeConnections)
		points = p.addCount(points, "db_connections_idle", counts["idle"], now, dbcommon.MetricTypeConnections)
		points = p.addCount(points, "db_connections_idle_in_transaction",
			counts["idle in transaction"]+counts["idle in transaction (aborted)"],
			now, dbcommon.MetricTypeConnections)
	}

	// max_connections — config, not state.
	var maxConn int64
	if err := p.db.QueryRowContext(ctx, "SHOW max_connections").Scan(&maxConn); err == nil {
		points = p.addCount(points, "db_connections_max", maxConn, now, dbcommon.MetricTypeConnections)
	}

	return points
}

// buildThroughputMetrics emits commits, rollbacks, queries
// (proxy = commits + rollbacks across non-system databases).
func (p *postgresqlProbe) buildThroughputMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	var commits, rollbacks int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(xact_commit), 0), COALESCE(SUM(xact_rollback), 0)
		 FROM pg_stat_database
		 WHERE datname NOT IN ('postgres','template0','template1') AND datname IS NOT NULL`,
	).Scan(&commits, &rollbacks)
	if err == nil {
		points = p.addCount(points, "db_transactions_committed", commits, now, dbcommon.MetricTypeThroughput)
		points = p.addCount(points, "db_transactions_rolled_back", rollbacks, now, dbcommon.MetricTypeThroughput)
		points = p.addCount(points, "db_queries_count", commits+rollbacks, now, dbcommon.MetricTypeThroughput)
	}

	return points
}

// buildCacheMetrics — buffer hit ratio derived from pg_stat_database.
func (p *postgresqlProbe) buildCacheMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	var hits, reads int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(blks_hit), 0), COALESCE(SUM(blks_read), 0)
		 FROM pg_stat_database
		 WHERE datname NOT IN ('postgres','template0','template1') AND datname IS NOT NULL`,
	).Scan(&hits, &reads)
	if err == nil {
		points = p.addRatio(points, "db_buffer_hit_ratio", hits, hits+reads, now, dbcommon.MetricTypeCache)
	}
	return points
}

// buildLocksMetrics emits deadlocks (counter) + waiting locks
// (gauge) + the SenHub long-running-transaction differentiator
// (DESIGN §5.7).
func (p *postgresqlProbe) buildLocksMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	var deadlocks int64
	if err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(deadlocks), 0) FROM pg_stat_database WHERE datname IS NOT NULL`,
	).Scan(&deadlocks); err == nil {
		points = p.addCount(points, "db_locks_deadlocks", deadlocks, now, dbcommon.MetricTypeLocks)
	}

	var waiting int64
	if err := p.db.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_locks WHERE granted = false",
	).Scan(&waiting); err == nil {
		points = p.addCount(points, "db_locks_waiting", waiting, now, dbcommon.MetricTypeLocks)
	}

	// Long-running transaction — age of the oldest open transaction.
	// NULL when no active transaction → 0.
	var longestSeconds *float64
	_ = p.db.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - MIN(xact_start)))
		 FROM pg_stat_activity
		 WHERE xact_start IS NOT NULL AND state <> 'idle'`,
	).Scan(&longestSeconds)
	v := float32(0)
	if longestSeconds != nil && *longestSeconds > 0 {
		v = float32(*longestSeconds)
		if !sanitize.IsFinite(v) {
			v = 0
		}
	}
	points = append(points, datapoint.DataPoint{
		Name: "db_postgres_long_running_xact_seconds", Timestamp: now, Value: v,
		Tags: p.commonTags(dbcommon.MetricTypeLocks),
	})

	return points
}

// buildStorageMetrics emits db_size_bytes (sum across user DBs)
// and db_tables_count.
func (p *postgresqlProbe) buildStorageMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	t := p.commonTags(dbcommon.MetricTypeStorage)

	var totalBytes int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(pg_database_size(datname)), 0)
		 FROM pg_database
		 WHERE datname NOT IN ('postgres','template0','template1')`,
	).Scan(&totalBytes)
	if err == nil {
		v, _ := sanitize.Bytes(totalBytes)
		points = append(points, datapoint.DataPoint{
			Name: "db_size_bytes", Timestamp: now, Value: v, Tags: t,
		})
	}

	var tableCount int64
	err = p.db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_stat_user_tables`).Scan(&tableCount)
	if err == nil {
		v, _ := sanitize.CountInt32(tableCount)
		points = append(points, datapoint.DataPoint{
			Name: "db_tables_count", Timestamp: now, Value: v, Tags: t,
		})
	}
	return points
}

// buildBackupsMetrics implements DESIGN §5.5 — backup freshness
// via pg_stat_archiver. The 'should-have-SenHub-differentiator'
// here is exposing this metric as a first-class channel rather
// than burying it in a custom dashboard query: operators get an
// obvious sensor for "is my disaster recovery working".
func (p *postgresqlProbe) buildBackupsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	t := p.commonTags(dbcommon.MetricTypeBackups)

	// pg_stat_archiver: row exists since PG 9.4; readable by
	// anyone with pg_monitor.
	var lastArchived *time.Time
	var failedCount int64
	err := p.db.QueryRowContext(ctx,
		"SELECT last_archived_time, failed_count FROM pg_stat_archiver",
	).Scan(&lastArchived, &failedCount)
	if err != nil {
		// Lacking grant or the archiver view is unavailable.
		return nil
	}

	if lastArchived != nil && !lastArchived.IsZero() {
		ageSeconds := float32(now.Sub(*lastArchived).Seconds())
		if ageSeconds < 0 {
			// Clock skew between agent and DB host — clamp to 0.
			ageSeconds = 0
		}
		if sanitize.IsFinite(ageSeconds) {
			points = append(points, datapoint.DataPoint{
				Name: "db_postgres_archiver_last_archived_age_seconds",
				Timestamp: now, Value: ageSeconds, Tags: t,
			})
		}
	}

	v, _ := sanitize.CountInt32(failedCount)
	points = append(points, datapoint.DataPoint{
		Name: "db_postgres_archiver_failed_count",
		Timestamp: now, Value: v, Tags: t,
	})

	return points
}

// buildPerDatabaseMetrics emits one db_database_size_bytes point
// per non-system database when expose_per_database is set.
func (p *postgresqlProbe) buildPerDatabaseMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	if !p.cfg.ExposePerDatabase {
		return nil
	}
	rows, err := p.db.QueryContext(ctx,
		"SELECT datname, pg_database_size(datname) FROM pg_database WHERE datistemplate = false")
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
			Name: "db_database_size_bytes", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}

// buildPerTableMetrics emits db_table_size_bytes for the top-N
// largest user relations when expose_top_tables > 0. The cap is
// enforced via LIMIT so the agent never streams every row.
func (p *postgresqlProbe) buildPerTableMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	if p.cfg.ExposeTopTables <= 0 {
		return nil
	}
	rows, err := p.db.QueryContext(ctx, `
		SELECT schemaname, relname, pg_relation_size(relid) AS size_bytes
		FROM pg_stat_user_tables
		ORDER BY size_bytes DESC
		LIMIT $1`, p.cfg.ExposeTopTables)
	if err != nil {
		p.logger.Warn().Err(err).Msg("per-table query failed")
		return nil
	}
	defer rows.Close()

	var points []datapoint.DataPoint
	for rows.Next() {
		var schema, rel string
		var sizeBytes int64
		if err := rows.Scan(&schema, &rel, &sizeBytes); err != nil {
			continue
		}
		tagsRow := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypePerTable)...)
		tagsRow = append(tagsRow,
			tags.Tag{Key: "schema", Value: schema},
			tags.Tag{Key: "table", Value: rel},
		)
		v, _ := sanitize.Bytes(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "db_table_size_bytes", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}
