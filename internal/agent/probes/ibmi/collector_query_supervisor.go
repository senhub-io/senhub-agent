package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// querySupervisorCollector observes the set of SQL queries currently
// running on the LPAR via QSYS2.ACTIVE_QUERY_INFO. A query here is an
// open ODP (Open Data Path) — the optimizer is actively feeding rows
// to a client. Long-running queries, queries that blow their time
// estimate, and queries that consume unreasonable temporary storage
// are the DBA's bread and butter.
//
// Cardinality is bounded by two knobs:
//   - a top-N cap (20) to keep per-query series manageable,
//   - aggregation by CURRENT_USER_NAME so the overall DB load by
//     originator is visible without per-row expansion.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-active-query-info
type querySupervisorCollector struct{}

func (querySupervisorCollector) Name() string  { return "query_supervisor" }
func (querySupervisorCollector) IsEvent() bool { return false }

const querySupervisorRowLimit = 20

func (querySupervisorCollector) SQL() string {
	// ORDER BY OPEN_DATE_TIME ASC puts the longest-running queries
	// at the top — they're the ones worth surfacing as a top-N.
	// COUNT(*) OVER () exposes the total concurrency in one pass.
	return fmt.Sprintf(
		"SELECT JOB_NAME, CURRENT_USER_NAME, CURRENT_TEMPORARY_STORAGE, OPEN_DATE_TIME, QUERY_TIME_ESTIMATE, ROWS_FETCHED, QUERY_TYPE, COUNT(*) OVER () AS TOTAL_ACTIVE FROM QSYS2.ACTIVE_QUERY_INFO ORDER BY OPEN_DATE_TIME ASC FETCH FIRST %d ROWS ONLY",
		querySupervisorRowLimit,
	)
}

func (querySupervisorCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	if len(res.Rows) == 0 {
		return []datapoint.DataPoint{{
			Name:      "ibmi.query_supervisor.active_total",
			Timestamp: ts,
			Value:     0,
			Tags:      []tags.Tag{hostTag},
		}}, nil
	}

	points := make([]datapoint.DataPoint, 0, len(res.Rows)*3+2)
	byUser := make(map[string]int, 8)
	var total float64

	for i, row := range res.Rows {
		jobName := trimmedCell(row, idx, "JOB_NAME")
		user := trimmedCell(row, idx, "CURRENT_USER_NAME")
		if user == "" {
			user = "<unknown>"
		}
		queryType := trimmedCell(row, idx, "QUERY_TYPE")
		byUser[user]++

		if i == 0 {
			if v, ok := parseFloatCell(row, idx, "TOTAL_ACTIVE"); ok {
				total = v
			}
		}

		tags := []tags.Tag{
			hostTag,
			{Key: "job_name", Value: jobName},
			{Key: "user", Value: user},
			{Key: "query_type", Value: queryType},
		}

		if v, ok := parseFloatCell(row, idx, "CURRENT_TEMPORARY_STORAGE"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.query_supervisor.temp_storage_mb",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
		if v, ok := parseFloatCell(row, idx, "ROWS_FETCHED"); ok {
			points = append(points, datapoint.DataPoint{
				Name:      "ibmi.query_supervisor.rows_fetched",
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}

		rawOpen, present, _ := requireCell(row, idx, "OPEN_DATE_TIME")
		if present && rawOpen != "" {
			if opened, err := time.ParseInLocation(ibmiTimestampLayout, rawOpen, time.Local); err == nil {
				elapsed := ts.Sub(opened).Seconds()
				if elapsed < 0 {
					elapsed = 0
				}
				points = append(points, datapoint.DataPoint{
					Name:      "ibmi.query_supervisor.elapsed_seconds",
					Timestamp: ts,
					Value:     float32(elapsed),
					Tags:      tags,
				})
			}
		}
	}

	for user, count := range byUser {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.query_supervisor.active_by_user",
			Timestamp: ts,
			Value:     float32(count),
			Tags:      []tags.Tag{hostTag, {Key: "user", Value: user}},
		})
	}
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.query_supervisor.active_total",
		Timestamp: ts,
		Value:     float32(total),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("ACTIVE_QUERY_INFO produced no usable datapoints")
	}
	return points, nil
}
