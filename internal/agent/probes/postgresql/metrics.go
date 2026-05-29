package postgresql

// metrics.go holds the engine-neutral helpers used by the family
// builders (families.go), the replication family (replication.go)
// and the SenHub differentiators (differentiators.go).

import (
	"context"
	"strconv"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/dbcommon"
	"senhub-agent.go/probesdk/sanitize"
	"senhub-agent.go/probesdk/tags"
)

// commonTags returns the systematic tags applied to every datapoint
// emitted by this probe instance.
//
// Three of these tags carry OTel-canonical semantic-conventions
// attribute names (db.system.name, server.address, server.port) and
// flow through as OTLP/Prometheus attributes via the IncludeProbeTags
// path of the mapper.
func (p *postgresqlProbe) commonTags(family dbcommon.MetricType) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: string(family)},
		{Key: "engine", Value: "postgresql"},
		{Key: "instance", Value: p.cfg.Host + ":" + strconv.Itoa(p.cfg.Port)},
		{Key: "environment", Value: string(p.environment)},
		{Key: "db.system.name", Value: "postgresql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
	}
}

// buildUpDatapoint emits senhub.db.up. Called both on a successful
// ping and on a failed ping so the dashboard always sees a fresh
// value.
func (p *postgresqlProbe) buildUpDatapoint(now time.Time, up bool) datapoint.DataPoint {
	v := float32(0)
	if up {
		v = 1
	}
	return datapoint.DataPoint{
		Name:      "senhub.db.up",
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

// addCount mirrors the MySQL probe's helper — wraps
// sanitize.CountInt32 so PRTG never gets an over-range int.
func (p *postgresqlProbe) addCount(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: p.commonTags(family),
	})
}

// addCountTagged is the variant for collapsed metrics whose internal
// names already include the discriminator suffix (e.g.
// postgresql.backends.active) but whose OTel mapping requires an
// attribute (state=active). The extra tag is appended on top of the
// family tags so the data_store layer carries it to the OTel mapper.
func (p *postgresqlProbe) addCountTagged(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType, tagKey, tagValue string) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	t := append([]tags.Tag{}, p.commonTags(family)...)
	t = append(t, tags.Tag{Key: tagKey, Value: tagValue})
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: t,
	})
}

// addRatio appends a ratio (gauge in [0,1]) when the denominator
// is non-zero. NaN and ±Inf are filtered.
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
