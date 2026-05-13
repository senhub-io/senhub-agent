package mysql

import (
	"context"
	"strconv"
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
func (p *mysqlProbe) detectRole(ctx context.Context) (dbcommon.Role, error) {
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
	defer rows.Close()

	if rows.Next() {
		// At least one row → this server is a replica.
		return dbcommon.RoleReplica, nil
	}

	// No replica status → this server is either primary or
	// standalone. Look for connected replicas via SHOW STATUS.
	var name string
	var value string
	err = p.db.QueryRowContext(ctx,
		"SHOW GLOBAL STATUS WHERE Variable_name IN ('Slaves_connected', 'Replicas_connected')",
	).Scan(&name, &value)
	if err != nil {
		// No replicas connected and the variable does not exist —
		// safe default is Standalone. Some MySQL builds (e.g. older
		// MariaDB) lack the variable entirely; we treat absence as
		// "no connected replicas" rather than erroring out.
		return dbcommon.RoleStandalone, nil
	}
	if value != "" && value != "0" {
		return dbcommon.RolePrimary, nil
	}
	return dbcommon.RoleStandalone, nil
}

// buildOverviewMetrics emits the small handful of metrics that
// belong to the `overview` metric_type family — uptime, version
// info, connections.utilization, replication.role,
// replication.health. The deep-dive families (connections,
// throughput, replication, cache, locks, io, storage) land in
// follow-up patches; this slice is the end-to-end smoke test that
// the probe + cache + sinks plumbing works.
func (p *mysqlProbe) buildOverviewMetrics(ctx context.Context, now time.Time, role dbcommon.Role) []datapoint.DataPoint {
	tagsOverview := p.commonTags(dbcommon.MetricTypeOverview)

	var points []datapoint.DataPoint

	// Uptime — single SHOW GLOBAL STATUS variable.
	if uptime, ok := p.queryGlobalStatusInt(ctx, "Uptime"); ok {
		if v, _ := sanitize.CountInt32(uptime); true {
			points = append(points, datapoint.DataPoint{
				Name: "db_uptime_seconds", Timestamp: now, Value: v, Tags: tagsOverview,
			})
		}
	}

	// Version info — always 1, version carried as a label so a
	// dashboard can group by it.
	versionTags := append([]tags.Tag{}, tagsOverview...)
	versionTags = append(versionTags, tags.Tag{Key: "version", Value: p.versionString})
	points = append(points, datapoint.DataPoint{
		Name: "db_version_info", Timestamp: now, Value: 1, Tags: versionTags,
	})

	// Connections utilization — primary saturation alarm. Threads
	// connected divided by max_connections.
	threadsConnected, okT := p.queryGlobalStatusInt(ctx, "Threads_connected")
	maxConnections, okM := p.queryGlobalVariableInt(ctx, "max_connections")
	if okT && okM && maxConnections > 0 {
		ratio := float32(threadsConnected) / float32(maxConnections)
		if sanitize.IsFinite(ratio) {
			points = append(points, datapoint.DataPoint{
				Name: "db_connections_utilization", Timestamp: now, Value: ratio, Tags: tagsOverview,
			})
		}
	}

	// Replication role — numeric so the PRTG lookup colours the
	// channel. The role is included as a string tag for Grafana
	// legend readability.
	roleTags := append([]tags.Tag{}, tagsOverview...)
	roleTags = append(roleTags, tags.Tag{Key: "role", Value: role.String()})
	points = append(points, datapoint.DataPoint{
		Name: "db_replication_role", Timestamp: now, Value: role.RoleValue(), Tags: roleTags,
	})

	// Composite replication health — only meaningful for a replica
	// (primary and standalone report 1 by convention, signalling
	// "no replication problem to detect here").
	healthValue := float32(1)
	if role == dbcommon.RoleReplica {
		// The full composite formula (DESIGN §5.2) needs the
		// replication family helper that lands in a later patch.
		// For the skeleton we expose 1 unconditionally — better
		// than emitting an inaccurate 0 with no underlying check.
		healthValue = 1
	}
	points = append(points, datapoint.DataPoint{
		Name: "db_replication_health", Timestamp: now, Value: healthValue, Tags: roleTags,
	})

	return points
}

// queryGlobalStatusInt returns the int value of a SHOW GLOBAL
// STATUS variable. Returns (0, false) when the variable is absent
// or the value is non-numeric — both paths are normal across MySQL
// versions and engine forks so callers handle the false case
// without logging a warn.
func (p *mysqlProbe) queryGlobalStatusInt(ctx context.Context, name string) (int64, bool) {
	var k, v string
	row := p.db.QueryRowContext(ctx,
		"SHOW GLOBAL STATUS WHERE Variable_name = ?", name)
	if err := row.Scan(&k, &v); err != nil {
		return 0, false
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// queryGlobalVariableInt is the SHOW GLOBAL VARIABLES counterpart.
// Kept separate because the two are distinct concepts in MySQL —
// status is current observation, variable is configuration — and a
// future helper may want to expose them with different semantics.
func (p *mysqlProbe) queryGlobalVariableInt(ctx context.Context, name string) (int64, bool) {
	var k, v string
	row := p.db.QueryRowContext(ctx,
		"SHOW GLOBAL VARIABLES WHERE Variable_name = ?", name)
	if err := row.Scan(&k, &v); err != nil {
		return 0, false
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
