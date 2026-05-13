package mysql

// replication.go isolates the replication family — the only one
// that needs both a free-form row read (SHOW REPLICA STATUS yields
// ~50 columns we project into a name→string map) and feeds the
// composite health value back to the overview emitter.

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

// replicationProbe holds the parsed signals from SHOW REPLICA
// STATUS that feed the composite health computation.
type replicationProbe struct {
	io         bool
	sql        bool
	lagSeconds float32 // -1 = unknown
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

// buildReplicationHealth emits db_replication_health under the
// overview family. The value is the composite 0/1 derived from the
// SHOW REPLICA STATUS signals (DESIGN §5.2). Standalone gets 1 by
// convention — "no replication problem to detect".
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
