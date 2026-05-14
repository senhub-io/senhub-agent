package postgresql

// differentiators.go holds the SenHub differentiator queries that
// have no equivalent on the MySQL side:
//
//   §5.3 — bloat estimate over top-N user tables
//   §5.4 — version-aware aggregate pg_stat_statements
//
// See docs/developer-guide/database-probes/DESIGN.md for the full
// rationale. Each builder skips silently if the underlying source
// is unavailable (no extension, missing grants) so the probe stays
// useful in stripped-down environments.

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/utils/sanitize"
)

// buildBloatMetrics implements DESIGN §5.3 — top-N tables bloat
// estimate using the dead-tuple ratio from pg_stat_user_tables (no
// extension required). Bounded by cfg.BloatTopN (default 10, hard
// cap 50 from parseConfig).
//
// The 'real' pgstattuple_approx() function gives a more accurate
// answer but requires the pgstattuple extension to be installed AND
// scans the actual heap, which is non-trivial on busy instances.
// We start with the no-extension approximation and can layer
// pgstattuple_approx() in later when the operator opts in.
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
		// Ratio = dead / (live + dead). Empty table → 0.
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

		tagsRow := append([]tags.Tag{}, p.commonTags(dbcommon.MetricTypeBloat)...)
		tagsRow = append(tagsRow,
			tags.Tag{Key: "schema", Value: schema},
			tags.Tag{Key: "table", Value: rel},
		)
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.postgresql.bloat.ratio", Timestamp: now, Value: ratio, Tags: tagsRow,
		})
		// Bloat in bytes — ratio applied to current heap size.
		// Approximation only; pgstattuple_approx() returns a more
		// precise number.
		bloatBytes := int64(float64(size) * float64(ratio))
		v, _ := sanitize.Bytes(bloatBytes)
		points = append(points, datapoint.DataPoint{
			Name: "senhub.db.postgresql.bloat.size", Timestamp: now, Value: v, Tags: tagsRow,
		})
	}
	return points
}

// buildStatStatementsMetrics implements DESIGN §5.4 — aggregate
// pg_stat_statements with column-name compatibility across PG
// versions. Returns nil silently when the extension is not
// installed; the metric is opt-in by extension install, not a
// config flag.
//
// Column rename history:
//   - PG ≤ 12: total_time
//   - PG 13+: total_exec_time (and total_plan_time, ignored)
//   - PG 17:  schema additions for top-N statements (ignored —
//     we only emit the aggregate, never per-statement)
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
		return nil
	}

	t := p.commonTags(dbcommon.MetricTypeStatStatements)
	var points []datapoint.DataPoint

	v, _ := sanitize.CountInt32(calls)
	points = append(points, datapoint.DataPoint{
		Name: "senhub.db.postgresql.statement.calls", Timestamp: now, Value: v, Tags: t,
	})

	if calls > 0 && totalMs != nil {
		// Source pg_stat_statements measures time in ms. OTel
		// canonical unit for durations is seconds — convert ÷ 1000.
		meanSeconds := float32(*totalMs / float64(calls) / 1000.0)
		if sanitize.IsFinite(meanSeconds) && meanSeconds >= 0 {
			points = append(points, datapoint.DataPoint{
				Name:      "senhub.db.postgresql.statement.exec_time.mean",
				Timestamp: now, Value: meanSeconds, Tags: t,
			})
		}
	}

	return points
}
