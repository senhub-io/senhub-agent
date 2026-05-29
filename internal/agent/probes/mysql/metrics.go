package mysql

// metrics.go holds the engine-neutral helpers used by the family
// builders (families.go) and the replication-specific code
// (replication.go). Adding a new family ⇒ add a new build*Metrics
// function in families.go ⇒ wire it into mysql.go's Collect.

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
// emitted by this probe instance. The metric_type tag is added per
// family by the helper that builds the family.
//
// Three of these tags carry OTel-canonical semantic-conventions
// attribute names (db.system.name, server.address, server.port) and
// are passed through as OTLP/Prometheus attributes by the mapper when
// IncludeProbeTags is enabled — which is the default for both sinks.
// This is how the probe gets its "resource-like" identity onto every
// datapoint without configuring resource attributes per-instance in
// the OTLP strategy config.
func (p *mysqlProbe) commonTags(family dbcommon.MetricType) []tags.Tag {
	return []tags.Tag{
		{Key: "metric_type", Value: string(family)},
		{Key: "engine", Value: "mysql"},
		{Key: "instance", Value: p.cfg.Host + ":" + strconv.Itoa(p.cfg.Port)},
		{Key: "environment", Value: string(p.environment)},
		{Key: "db.system.name", Value: "mysql"},
		{Key: "server.address", Value: p.cfg.Host},
		{Key: "server.port", Value: strconv.Itoa(p.cfg.Port)},
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
		Name:      "senhub.db.up",
		Timestamp: now,
		Value:     v,
		Tags:      p.commonTags(dbcommon.MetricTypeOverview),
	}
}

// detectRole maps the SHOW SLAVE STATUS result + the bulk status
// map's Slaves_connected variable into the Role enum (DESIGN §5.1).
// Returns RoleStandalone + error on failure; callers log the error
// and fall back to Standalone (the safe default that means "no
// replication metrics for this scrape").
func (p *mysqlProbe) detectRole(ctx context.Context, status map[string]string) (dbcommon.Role, error) {
	// MySQL 8.0.22+ prefers SHOW REPLICA STATUS; older versions
	// only know SHOW SLAVE STATUS. Try the newer one first; on
	// syntax error fall back to the legacy form.
	rows, err := p.db.QueryContext(ctx, "SHOW REPLICA STATUS")
	if err != nil {
		rows, err = p.db.QueryContext(ctx, "SHOW SLAVE STATUS")
		if err != nil {
			return dbcommon.RoleStandalone, err
		}
	}
	hasReplicaStatus := rows.Next()
	rows.Close()

	if hasReplicaStatus {
		return dbcommon.RoleReplica, nil
	}

	// No replica status → primary or standalone. Inspect the bulk
	// status map for connected replicas; the variable name differs
	// between MySQL 5.7, 8.0 and MariaDB.
	for _, k := range []string{"Slaves_connected", "Replicas_connected"} {
		if v, ok := status[k]; ok && v != "" && v != "0" {
			return dbcommon.RolePrimary, nil
		}
	}
	return dbcommon.RoleStandalone, nil
}

// fetchGlobalStatus runs a single SHOW GLOBAL STATUS and returns
// the result as a name→value map. One bulk query per cycle is
// cheaper than N targeted queries (each `SHOW GLOBAL STATUS WHERE
// Variable_name = …` is its own round-trip), so every family
// helper reads from this map.
func (p *mysqlProbe) fetchGlobalStatus(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, "SHOW GLOBAL STATUS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, 512)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}

// fetchGlobalVariables mirrors fetchGlobalStatus for SHOW GLOBAL
// VARIABLES. Variables are configuration (max_connections,
// long_query_time, …); STATUS is observation. They live in
// different SHOW commands and the probe needs both.
func (p *mysqlProbe) fetchGlobalVariables(ctx context.Context) (map[string]string, error) {
	rows, err := p.db.QueryContext(ctx, "SHOW GLOBAL VARIABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string, 512)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, rows.Err()
}

// asInt parses a value from a status/variable map. Returns
// (0, false) on missing or non-numeric — both are normal across
// MySQL versions (variables come and go) so callers handle the
// false case as "metric not available right now" without warning.
func asInt(m map[string]string, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// asFloat is the float64 counterpart for variables that carry
// non-integer values (e.g. long_query_time).
func asFloat(m map[string]string, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// addCount appends a counter/gauge datapoint with the standard
// family tags. Wraps sanitize.CountInt32 so PRTG never receives an
// over-range integer.
func (p *mysqlProbe) addCount(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: p.commonTags(family),
	})
}

// addCountTagged is the variant for collapsed metrics whose internal
// names already include the discriminator suffix (e.g.
// mysql.threads.connected) but whose OTel mapping requires an
// attribute (kind=connected). The extra tag is appended on top of the
// family tags so the data_store layer ships it through to the OTel
// mapper without losing it on the cache step.
func (p *mysqlProbe) addCountTagged(points []datapoint.DataPoint, name string, raw int64, ts time.Time, family dbcommon.MetricType, tagKey, tagValue string) []datapoint.DataPoint {
	v, _ := sanitize.CountInt32(raw)
	t := append([]tags.Tag{}, p.commonTags(family)...)
	t = append(t, tags.Tag{Key: tagKey, Value: tagValue})
	return append(points, datapoint.DataPoint{
		Name: name, Timestamp: ts, Value: v, Tags: t,
	})
}

// addRatio appends a ratio (gauge in [0,1]) when the denominator
// is non-zero. NaN and ±Inf are filtered.
func (p *mysqlProbe) addRatio(points []datapoint.DataPoint, name string, num, den int64, ts time.Time, family dbcommon.MetricType) []datapoint.DataPoint {
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

// stringifyRaw normalises a rows.Scan(&interface{}) result into a
// string. database/sql may return []byte for VARCHAR depending on
// the driver; SHOW REPLICA STATUS is full of stringly-typed columns
// so we coerce defensively rather than assert a specific type.
func stringifyRaw(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return strconv.FormatInt(0, 10)
	}
}
