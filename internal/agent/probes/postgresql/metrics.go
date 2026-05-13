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
// emitted on standalone (auto role-detect contract).
func (p *postgresqlProbe) buildReplicationMetrics(ctx context.Context, now time.Time, role dbcommon.Role) []datapoint.DataPoint {
	if role == dbcommon.RoleStandalone {
		return nil
	}
	t := p.commonTags(dbcommon.MetricTypeReplication)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})

	var points []datapoint.DataPoint

	if role == dbcommon.RoleReplica {
		// Lag in seconds. NULL when the replica is fully caught
		// up — coerce to 0 in that case (caught-up == 0s lag).
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
