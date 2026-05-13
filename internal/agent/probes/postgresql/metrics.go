package postgresql

import (
	"context"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

func (p *postgresqlProbe) commonTags(family dbcommon.MetricType) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: string(family)},
		{Key: "engine", Value: "postgresql"},
		{Key: "instance", Value: p.cfg.Host + ":" + strconv.Itoa(p.cfg.Port)},
		{Key: "environment", Value: string(p.environment)},
	}
}

func (p *postgresqlProbe) buildUpDatapoint(now time.Time, up bool) datapoint.DataPoint {
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

// detectRole — pg_is_in_recovery() for replica detection, count of
// pg_stat_replication for primary-with-replicas. See DESIGN §5.1.
func (p *postgresqlProbe) detectRole(ctx context.Context) (dbcommon.Role, error) {
	var inRecovery bool
	if err := p.db.QueryRowContext(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		return dbcommon.RoleStandalone, err
	}
	if inRecovery {
		return dbcommon.RoleReplica, nil
	}
	var n int
	if err := p.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_replication").Scan(&n); err != nil {
		// pg_stat_replication requires pg_monitor or superuser.
		// Without it we still know this is not a replica → treat
		// as standalone (safe default).
		return dbcommon.RoleStandalone, nil
	}
	if n > 0 {
		return dbcommon.RolePrimary, nil
	}
	return dbcommon.RoleStandalone, nil
}

// addCount / addRatio mirror their MySQL siblings.
func (p *postgresqlProbe) addCount(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: p.commonTags(family),
	})
}

func (p *postgresqlProbe) addRatio(points []datapoint.DataPoint, name string, num, den int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
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

	// One query, grouped by state, gives us the full breakdown.
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

// buildReplicationMetrics emits the replication family. Nothing
// emitted on standalone (auto role-detect contract). Returns the
// composite health value so the caller can wire it into
// db_replication_health (see DESIGN §5.2). Standalone reports 1
// by convention.
func (p *postgresqlProbe) buildReplicationMetrics(ctx context.Context, now time.Time, role dbcommon.Role) ([]datapoint.DataPoint, float32) {
	if role == dbcommon.RoleStandalone {
		return nil, 1
	}
	t := p.commonTags(dbcommon.MetricTypeReplication)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})

	var points []datapoint.DataPoint
	health := float32(1)

	if role == dbcommon.RoleReplica {
		// Lag in seconds. NULL when the replica is fully caught
		// up — coerce to 0 (caught-up == 0s lag).
		var lagSecondsNullable *float64
		_ = p.db.QueryRowContext(ctx,
			"SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))",
		).Scan(&lagSecondsNullable)
		lag := float32(0)
		if lagSecondsNullable != nil {
			lag = float32(*lagSecondsNullable)
			if lag < 0 || !sanitize.IsFinite(lag) {
				lag = 0
			}
		}
		points = append(points, datapoint.DataPoint{
			Name: "db_replication_lag_seconds", Timestamp: now, Value: lag, Tags: roleTagged,
		})

		// IO thread health — pg_stat_wal_receiver.status='streaming'
		// when healthy. Absent row = receiver crashed.
		var status string
		err := p.db.QueryRowContext(ctx,
			"SELECT status FROM pg_stat_wal_receiver LIMIT 1",
		).Scan(&status)
		ioRunning := float32(0)
		if err == nil && status == "streaming" {
			ioRunning = 1
		}
		points = append(points, datapoint.DataPoint{
			Name: "db_replication_io_running", Timestamp: now, Value: ioRunning, Tags: roleTagged,
		})

		// Composite health: io_running AND lag<threshold.
		if ioRunning == 0 || lag > float32(p.cfg.MaxReplicationLagSeconds) {
			health = 0
		}
	}

	// Replicas connected (primary).
	if role == dbcommon.RolePrimary {
		var n int64
		if err := p.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_replication").Scan(&n); err == nil {
			v, _ := sanitize.CountInt32(n)
			points = append(points, datapoint.DataPoint{
				Name: "db_replication_replicas_connected", Timestamp: now, Value: v, Tags: roleTagged,
			})
		}
	}

	return points, health
}

// buildReplicationHealth emits db_replication_health under the
// overview family — same shape as the MySQL probe.
func (p *postgresqlProbe) buildReplicationHealth(now time.Time, role dbcommon.Role, health float32) datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})
	return datapoint.DataPoint{
		Name: "db_replication_health", Timestamp: now, Value: health, Tags: roleTagged,
	}
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
// (gauge) + the SenHub long-running-transaction differentiator.
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

	// Long-running transaction (DESIGN §5.7) — age of the oldest
	// open transaction. NULL when no active transaction → 0.
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
		v, _ := sanitize.BytesInt32(totalBytes)
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

// buildBloatMetrics implements DESIGN §5.3 — top-N tables bloat
// estimate. Uses the dead-tuple ratio approximation from
// pg_stat_user_tables (no extension required). Bounded by
// cfg.BloatTopN (default 10, hard cap 50 from parseConfig).
//
// The "real" pgstattuple_approx() function gives a more accurate
// answer but requires the pgstattuple extension to be installed
// AND scans the actual heap, which is non-trivial on busy
// instances. We start with the no-extension approximation and
// can layer pgstattuple_approx() in later when the operator
// opts in.
func (p *postgresqlProbe) buildBloatMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	if p.cfg.BloatTopN <= 0 {
		return nil
	}
	rows, err := p.db.QueryContext(ctx, `
		SELECT schemaname, relname, n_live_tup, n_dead_tup, pg_relation_size(relid) AS size_bytes
		FROM pg_stat_user_tables
		ORDER BY size_bytes DESC
		LIMIT $1`, p.cfg.BloatTopN)
	if err != nil {
		// pg_stat_user_tables requires only basic select; failure
		// here usually means the role lacks pg_monitor on managed
		// DBs. Warn once and skip.
		p.logger.Warn().Err(err).Msg("bloat estimate skipped — pg_stat_user_tables unreadable")
		return nil
	}
	defer rows.Close()

	var points []datapoint.DataPoint
	for rows.Next() {
		var schema, rel string
		var live, dead, size int64
		if err := rows.Scan(&schema, &rel, &live, &dead, &size); err != nil {
			continue
		}
		// Ratio = dead / (live + dead). When the table is empty
		// (live+dead = 0) we report 0 — no bloat to claim.
		total := live + dead
		ratio := float32(0)
		if total > 0 {
			ratio = float32(dead) / float32(total)
		}
		if !sanitize.IsFinite(ratio) || ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}

		tagsRow := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypeStorage)...)
		tagsRow = append(tagsRow,
			tags.Tag{Key: "schema", Value: schema},
			tags.Tag{Key: "relation", Value: rel},
		)
		points = append(points, datapoint.DataPoint{
			Name: "db_postgres_bloat_ratio", Timestamp: now, Value: ratio, Tags: tagsRow,
		})
		// Bloat in bytes — ratio applied to current heap size.
		// Approximation only; pgstattuple_approx() returns a
		// more precise number.
		bloatBytes := int64(float64(size) * float64(ratio))
		v, _ := sanitize.BytesInt32(bloatBytes)
		points = append(points, datapoint.DataPoint{
			Name: "db_postgres_bloat_bytes", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}

// buildStatStatementsMetrics implements DESIGN §5.4 — aggregate
// pg_stat_statements with column-name compatibility across PG
// versions. Returns nil silently when the extension is not
// installed; an info log fires once at OnStart (TODO) if the
// operator expects these metrics.
//
// Column rename history:
//   - PG ≤ 12: total_time
//   - PG 13+: total_exec_time (and total_plan_time, ignored)
//   - PG 17:  schema additions for top-N statements (ignored —
//             we only emit the aggregate, never per-statement)
func (p *postgresqlProbe) buildStatStatementsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	// Column selection picks itself based on server_version_num
	// captured at OnStart. 130000 = PG 13.0.
	totalTimeCol := "total_time"
	if p.versionNum >= 130000 {
		totalTimeCol = "total_exec_time"
	}

	// One aggregate row. The extension may exist but be empty
	// (pg_stat_statements_reset() just ran) — handle 0 calls
	// gracefully.
	var calls int64
	var totalMs *float64 // pg_stat_statements time is in ms
	err := p.db.QueryRowContext(ctx,
		"SELECT COALESCE(SUM(calls), 0), SUM("+totalTimeCol+") FROM pg_stat_statements",
	).Scan(&calls, &totalMs)
	if err != nil {
		// Extension absent or not in shared_preload_libraries.
		// Silent skip — the metric is opt-in by extension install.
		return nil
	}

	t := p.commonTags(dbcommon.MetricTypeEngine)
	var points []datapoint.DataPoint

	v, _ := sanitize.CountInt32(calls)
	points = append(points, datapoint.DataPoint{
		Name: "db_postgres_stat_statements_calls_count", Timestamp: now, Value: v, Tags: t,
	})

	if calls > 0 && totalMs != nil {
		mean := float32(*totalMs / float64(calls))
		if sanitize.IsFinite(mean) && mean >= 0 {
			points = append(points, datapoint.DataPoint{
				Name: "db_postgres_stat_statements_exec_time_mean_ms",
				Timestamp: now, Value: mean, Tags: t,
			})
		}
	}

	return points
}

// buildBackupsMetrics implements DESIGN §5.5 — backup freshness
// for PostgreSQL. Uses pg_stat_archiver.last_archived_time as the
// canary for WAL archiving (DR pipeline). When WAL archiving is
// not configured the view's row exists but last_archived_time is
// NULL — we skip the metric in that case (a "no backup pipeline"
// signal is itself useful but it's already inferable from the
// archiver count being 0).
//
// The 'should-have-SenHub-differentiator' here is exposing this
// metric as a first-class channel rather than burying it in a
// custom dashboard query — operators get an obvious sensor for
// "is my disaster recovery working".
func (p *postgresqlProbe) buildBackupsMetrics(ctx context.Context, now time.Time) []datapoint.DataPoint {
	var points []datapoint.DataPoint
	t := p.commonTags(dbcommon.MetricTypeBackups)

	// Last archived WAL file timestamp + failure count. Both come
	// from pg_stat_archiver, which exists since PG 9.4 and is
	// readable by anyone with pg_monitor.
	var lastArchived *time.Time
	var failedCount int64
	err := p.db.QueryRowContext(ctx,
		"SELECT last_archived_time, failed_count FROM pg_stat_archiver",
	).Scan(&lastArchived, &failedCount)
	if err != nil {
		// Most likely cause: lacking grant or the archiver view
		// is unavailable (very old PG or stripped-down fork).
		// Silent skip.
		return nil
	}

	if lastArchived != nil && !lastArchived.IsZero() {
		ageSeconds := float32(now.Sub(*lastArchived).Seconds())
		if ageSeconds < 0 {
			// Clock skew between agent and DB host. Clamp to 0
			// rather than emit a negative age which dashboards
			// can't represent.
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
