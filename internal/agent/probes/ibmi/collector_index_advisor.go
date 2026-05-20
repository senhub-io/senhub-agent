package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// indexAdvisorCollector surfaces IBM i's own recommendations for
// missing indexes from QSYS2.SYSIXADV. The advisor is populated
// automatically as the query optimizer encounters scans that would
// have benefited from an index — it's a near-zero-cost signal that
// is widely under-used in IBM i shops. Exposing it in the monitoring
// stack is a differentiation argument vs. products that stop at
// surface-level DB metrics.
//
// Top-20 rows by TIMES_ADVISED are emitted as per-advice DataPoints
// (table identity + key columns as tags) along with a host-wide total
// computed via COUNT(*) OVER () so we don't need a second SQL to get
// the cardinality of the advisor table.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=sqlqueries-database-monitor-views#rzajqviewsysixadv
type indexAdvisorCollector struct{}

func (indexAdvisorCollector) Name() string  { return "index_advisor" }
func (indexAdvisorCollector) IsEvent() bool { return false }

// indexAdvisorRowLimit is the top-N cap. Twenty is enough to surface
// the hottest advisories on a busy LPAR without flooding the time
// series; production deployments that want the long tail can add a
// second instance with a higher cap via config in the future.
const indexAdvisorRowLimit = 20

func (indexAdvisorCollector) SQL() string {
	// COUNT(*) OVER () is a Db2 for i supported window function that
	// lets us compute the total row count in the same result set as
	// the top-N slice — avoiding a second round-trip.
	return fmt.Sprintf(
		"SELECT TABLE_SCHEMA, TABLE_NAME, KEY_COLUMNS_ADVISED, TIMES_ADVISED, MTI_USED, MTI_CREATED, LAST_ADVISED, AVERAGE_QUERY_ESTIMATE, COUNT(*) OVER () AS TOTAL_ADVISED FROM QSYS2.SYSIXADV ORDER BY TIMES_ADVISED DESC FETCH FIRST %d ROWS ONLY",
		indexAdvisorRowLimit,
	)
}

func (indexAdvisorCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		// Empty result is a valid state: either the system has never
		// run anything that needed advising (rare in practice), or
		// SYSIXADV has been cleared. Emit a zero-total so the
		// dashboard line stays continuous.
		return []datapoint.DataPoint{{
			Name:      "ibmi.index_advisor.total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*3+2)
	var total float64
	// recentAdvisories counts advisories whose LAST_ADVISED landed in
	// the last hour — a useful leading indicator of ongoing query
	// pain (vs the long-lived entries that have been there for weeks).
	recentAdvisories := 0
	recentCutoff := ts.Add(-time.Hour)

	for i, row := range res.Rows {
		schema := trimmedCell(row, idx, "TABLE_SCHEMA")
		table := trimmedCell(row, idx, "TABLE_NAME")
		if table == "" {
			continue
		}
		keyColumns := trimmedCell(row, idx, "KEY_COLUMNS_ADVISED")

		// TOTAL_ADVISED is identical across rows (window function) —
		// pick it once from the first row.
		if i == 0 {
			if v, ok := parseFloatCell(row, idx, "TOTAL_ADVISED"); ok {
				total = v
			}
		}

		tags := []tags.Tag{
			hostTag,
			{Key: "table_schema", Value: schema},
			{Key: "table_name", Value: table},
			{Key: "key_columns", Value: keyColumns},
		}

		if v, ok := parseFloatCell(row, idx, "TIMES_ADVISED"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.index_advisor.times_advised",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "MTI_USED"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.index_advisor.mti_used",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "AVERAGE_QUERY_ESTIMATE"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.index_advisor.avg_query_estimate_seconds",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}

		rawLast, present, _ := requireCell(row, idx, "LAST_ADVISED")
		if present && rawLast != "" {
			if lastAdvised, err := time.ParseInLocation(ibmiTimestampLayout, rawLast, time.Local); err == nil {
				if lastAdvised.After(recentCutoff) {
					recentAdvisories++
				}
			}
		}
	}

	points = append(points,
		datapoint.DataPoint{
			Name:      "ibmi.index_advisor.total",
			Timestamp: ts,
			Value:     float32(total),
			Tags:      []tags.Tag{hostTag},
		},
		datapoint.DataPoint{
			Name:      "ibmi.index_advisor.recent_advisories_1h",
			Timestamp: ts,
			Value:     float32(recentAdvisories),
			Tags:      []tags.Tag{hostTag},
		},
	)

	if len(points) == 0 {
		return nil, fmt.Errorf("SYSIXADV produced no usable datapoints")
	}
	return points, nil
}
