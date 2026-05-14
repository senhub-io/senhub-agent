package postgresql

// replication.go isolates the replication family for symmetry
// with the MySQL probe, and because both ends of the design
// contract (auto role-detect + composite health, DESIGN §5.1 +
// §5.2) flow through this single function.

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

// buildReplicationMetrics emits the replication family. Nothing
// emitted on standalone (auto role-detect contract). Returns the
// composite health value so the caller can wire it into
// db_replication_health (DESIGN §5.2). Standalone reports 1 by
// convention.
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
		// postgresql.wal.lag with operation=replay (contrib canon).
		lagTagged := append([]tags.Tag{}, roleTagged...)
		lagTagged = append(lagTagged, tags.Tag{Key: "operation", Value: "replay"})
		points = append(points, datapoint.DataPoint{
			Name: "postgresql.wal.lag", Timestamp: now, Value: lag, Tags: lagTagged,
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
			Name: "senhub.db.postgresql.replica.io.running", Timestamp: now, Value: ioRunning, Tags: roleTagged,
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
				Name: "senhub.db.replication.replicas.connected", Timestamp: now, Value: v, Tags: roleTagged,
			})
		}
	}

	return points, health
}

// buildReplicationHealth emits senhub.db.replication.health under
// the overview family — same shape as the MySQL probe.
func (p *postgresqlProbe) buildReplicationHealth(now time.Time, role dbcommon.Role, health float32) datapoint.DataPoint {
	t := p.commonTags(dbcommon.MetricTypeOverview)
	roleTagged := append([]tags.Tag{}, t...)
	roleTagged = append(roleTagged, tags.Tag{Key: "role", Value: role.String()})
	return datapoint.DataPoint{
		Name: "senhub.db.replication.health", Timestamp: now, Value: health, Tags: roleTagged,
	}
}
