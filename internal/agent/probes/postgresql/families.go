package postgresql

// families.go holds the per-metric_type builders for everything
// except the replication family (replication.go) and the SenHub
// differentiators bloat / pg_stat_statements (differentiators.go).
//
// Metric naming follows the OTel-first conventions defined in
// docs/developer-guide/otel/senhub-semantic-conventions.md §4.13.
// Internal probe metric names match the YAML transformer entries
// (postgresql.*, senhub.db.*, senhub.db.postgresql.*) — see
// postgresql.yaml for the canonical list and OTel mapping.
//
// Volontary asymmetry with the mysql probe: transactions are emitted
// as `postgresql.commits` + `postgresql.rollbacks` (contrib canon)
// rather than `senhub.db.<engine>.transaction.count{state}`. See
// senhub-semantic-conventions.md §4.13.5 for the rationale.

import (
	"context"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/dbcommon"
	"senhub-agent.go/probesdk/sanitize"
	"senhub-agent.go/probesdk/tags"
)

// buildOverviewMetrics emits uptime, version info, connections
// utilization, replication role.
func (p *postgresqlProbe) buildOverviewMetrics(ctx context.Context, now time.Time, role dbcommon.Role) []datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	var points []datapoint.DataPoint

	// Uptime via pg_postmaster_start_time(). Gauge — contrib postgres
	// receiver doesn't expose uptime so we keep it under senhub.db.*.
	var uptimeSeconds float64
	if err := p.db.QueryRowContext(ctx,
		"SELECT EXTRACT(EPOCH FROM (now() - pg_postmaster_start_time()))",
	).Scan(&uptimeSeconds); err == nil {
		v, _ := sanitize.CountInt32(int64(uptimeSeconds))
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.postgresql.uptime", Timestamp: now, Value: v, Tags: t,
		})
	}

	// Version info — value 1, version string carried as tag that the
	// YAML's tag_to_attribute lifts into db.system.version on OTLP.
	versionTags := append([]tags.Tag{}, t...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "senhub.db.version.info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization — count(pg_stat_activity) /
	// max_connections.
	var active, maxConn int64
	_ = p.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity").Scan(&active)
	_ = p.db.QueryRowContext(ctx, "SELECT current_setting('max_connections')::int").Scan(&maxConn)
	if active > 0 && maxConn > 0 {
		points = p.addRatio(points, "senhub.db.connection.utilization", active, maxConn, now, dbcommon.MetricTypeOverview)
	}

	// Replication role (raw enum — mapper expands via otel.expand).
	roleTags := append([]tags.Tag{}, t...)
	roleTags = append(roleTags, tags.Tag{Key: "role", Value: role.String()})
	points = append(points, datapoint.DataPoint{
		Name: "senhub.db.replication.role", Timestamp: now, Value: role.RoleValue(), Tags: roleTags,
	})

	return points
}

// buildConnectionsMetrics emits the breakdown by state. The three
// states (active, idle, idle_in_transaction) collapse into a single
// OTel metric `postgresql.backends` via the `state` attribute (canon
// contrib).
func (p *postgresqlProbe) buildConnectionsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

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
		if err := rows.Err(); err != nil {
			p.logger.Warn().Err(err).Msg("connections row scan interrupted; partial counts may flow this cycle")
		}
		points = p.addCountTagged(points, "postgresql.backends.active", counts["active"], now, dbcommon.MetricTypeConnections, "state", "active")
		points = p.addCountTagged(points, "postgresql.backends.idle", counts["idle"], now, dbcommon.MetricTypeConnections, "state", "idle")
		points = p.addCountTagged(points, "postgresql.backends.idle_in_transaction",
			counts["idle in transaction"]+counts["idle in transaction (aborted)"],
			now, dbcommon.MetricTypeConnections, "state", "idle_in_transaction")
	}

	// max_connections — config, not state.
	var maxConn int64
	if err := p.db.QueryRowContext(ctx, "SELECT current_setting('max_connections')::int").Scan(&maxConn); err == nil {
		points = p.addCount(points, "postgresql.connection.max", maxConn, now, dbcommon.MetricTypeConnections)
	}

	return points
}

// buildThroughputMetrics emits commits and rollbacks separately
// (contrib canon `postgresql.commits` / `postgresql.rollbacks`).
// The aggregate `queries` metric is gone — dashboards add them via
// PromQL if needed.
func (p *postgresqlProbe) buildThroughputMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	var commits, rollbacks int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(xact_commit), 0), COALESCE(SUM(xact_rollback), 0)
		 FROM pg_stat_database
		 WHERE datname NOT IN ('postgres','template0','template1') AND datname IS NOT NULL`,
	).Scan(&commits, &rollbacks)
	if err == nil {
		points = p.addCount(points, "postgresql.commits", commits, now, dbcommon.MetricTypeThroughput)
		points = p.addCount(points, "postgresql.rollbacks", rollbacks, now, dbcommon.MetricTypeThroughput)
	}

	return points
}

// buildCacheMetrics — buffer hit ratio derived from pg_stat_database.
// Contrib expose les compteurs bruts (blks_hit/blks_read) ; on émet
// ici uniquement le ratio dérivé sous senhub.db.postgresql.* pour les
// dashboards 'santé'.
func (p *postgresqlProbe) buildCacheMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	var hits, reads int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(blks_hit), 0), COALESCE(SUM(blks_read), 0)
		 FROM pg_stat_database
		 WHERE datname NOT IN ('postgres','template0','template1') AND datname IS NOT NULL`,
	).Scan(&hits, &reads)
	if err == nil {
		points = p.addRatio(points, "senhub.db.postgresql.buffer.hit_ratio", hits, hits+reads, now, dbcommon.MetricTypeCache)
	}
	return points
}

// buildLocksMetrics emits deadlocks (contrib `postgresql.deadlocks`),
// waiting locks (extension) and the SenHub long-running-transaction
// differentiator.
func (p *postgresqlProbe) buildLocksMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint

	var deadlocks int64
	if err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(deadlocks), 0) FROM pg_stat_database WHERE datname IS NOT NULL`,
	).Scan(&deadlocks); err == nil {
		points = p.addCount(points, "postgresql.deadlocks", deadlocks, now, dbcommon.MetricTypeLocks)
	}

	var waiting int64
	if err := p.db.QueryRowContext(ctx,
		"SELECT count(*) FROM pg_locks WHERE granted = false",
	).Scan(&waiting); err == nil {
		points = p.addCount(points, "senhub.db.postgresql.lock.waiting", waiting, now, dbcommon.MetricTypeLocks)
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
		Name: "senhub.db.postgresql.long_running_xact", Timestamp: now, Value: v,
		Tags: p.commonTags(dbcommon.MetricTypeLocks),
	})

	return points
}

// buildStorageMetrics emits postgresql.db_size (sum across user DBs)
// and postgresql.table.count.
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
			Name: "postgresql.db_size", Timestamp: now, Value: v, Tags: t,
		})
	}

	var tableCount int64
	err = p.db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_stat_user_tables`).Scan(&tableCount)
	if err == nil {
		v, _ := sanitize.CountInt32(tableCount)
		points = append(points, datapoint.DataPoint{
			Name: "postgresql.table.count", Timestamp: now, Value: v, Tags: t,
		})
	}
	return points
}

// buildBackupsMetrics implements DESIGN §5.5 — WAL archiver
// freshness + cumulative failures. The metric is opt-in by
// archive_mode (when archive_mode is off, the row exists but
// last_archived_time is NULL, in which case we only emit the
// failed_count counter).
func (p *postgresqlProbe) buildBackupsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	t := p.commonTags(dbcommon.MetricTypeArchiver)

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
				Name:      "senhub.db.postgresql.archiver.last_archived.age",
				Timestamp: now, Value: ageSeconds, Tags: t,
			})
		}
	}

	v, _ := sanitize.CountInt32(failedCount)
	points = append(points, datapoint.DataPoint{
		Name:      "senhub.db.postgresql.archiver.failed",
		Timestamp: now, Value: v, Tags: t,
	})

	return points
}

// buildPerDatabaseMetrics emits postgresql.db_size.per_database for
// each non-system database when expose_per_database is set.
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
			Name: "postgresql.db_size.per_database", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	if err := rows.Err(); err != nil {
		p.logger.Warn().Err(err).Msg("per-database row scan interrupted; partial results may flow this cycle")
	}
	return points
}

// buildPerTableMetrics emits postgresql.table.size for the top-N
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
			tags.Tag{Key: "database", Value: schema},
			tags.Tag{Key: "table", Value: rel},
		)
		v, _ := sanitize.Bytes(sizeBytes)
		points = append(points, datapoint.DataPoint{
			Name: "postgresql.table.size", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	if err := rows.Err(); err != nil {
		p.logger.Warn().Err(err).Msg("per-table row scan interrupted; partial results may flow this cycle")
	}
	return points
}
