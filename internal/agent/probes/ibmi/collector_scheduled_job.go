package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// scheduledJobCollector summarises the WRKJOBSCDE catalogue via
// QSYS2.SCHEDULED_JOB_INFO. It emits two dimensions: a count per
// STATUS (SCD / HLD / SAV) as coarse health, and per-entry gauges
// showing the age of the last successful submission. A stale
// scheduled job is a common cause of silent outages and worth
// surfacing in a dashboard at a glance.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-scheduled-job-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type scheduledJobCollector struct{}

func (scheduledJobCollector) Name() string  { return "scheduled_job" }
func (scheduledJobCollector) IsEvent() bool { return false }

func (scheduledJobCollector) SQL() string {
	return "SELECT SCHEDULED_JOB_NAME, STATUS, FREQUENCY, NEXT_SUBMISSION_DATE, LAST_SUCCESSFUL_SUBMISSION_TIMESTAMP, LAST_ATTEMPTED_SUBMISSION_STATUS FROM QSYS2.SCHEDULED_JOB_INFO"
}

func (scheduledJobCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	if len(res.Rows) == 0 {
		// Zero scheduled jobs is valid on a fresh LPAR. Still emit
		// the aggregate zero so the series stays continuous.
		return []datapoint.DataPoint{
			{
				Name:      "ibmi.scheduled_job.total",
				Timestamp: ts,
				Value:     0,
				Tags:      []tags.Tag{{Key: "host", Value: host}},
			},
		}, nil
	}

	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}
	statusCounts := make(map[string]int, 4)
	points := make([]datapoint.DataPoint, 0, len(res.Rows)+8)

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "SCHEDULED_JOB_NAME")
		if name == "" {
			continue
		}
		status := trimmedCell(row, idx, "STATUS")
		frequency := trimmedCell(row, idx, "FREQUENCY")
		lastStatus := trimmedCell(row, idx, "LAST_ATTEMPTED_SUBMISSION_STATUS")
		statusCounts[status]++

		tags := []tags.Tag{
			hostTag,
			{Key: "job_name", Value: name},
			{Key: "status", Value: status},
			{Key: "frequency", Value: frequency},
			{Key: "last_attempt_status", Value: lastStatus},
		}

		// last_run_age_seconds: time elapsed since the last successful
		// submission. A scheduled job with no successful submission
		// ever (new entry or permanently broken) emits -1 so the
		// gauge is visually distinguishable.
		lastTs := trimmedCell(row, idx, "LAST_SUCCESSFUL_SUBMISSION_TIMESTAMP")
		var ageSeconds float64 = -1
		if lastTs != "" {
			if parsed, err := time.ParseInLocation(ibmiTimestampShortLayout, lastTs, time.Local); err == nil {
				ageSeconds = ts.Sub(parsed).Seconds()
			}
		}
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.scheduled_job.last_run_age_seconds",
			Timestamp: ts,
			Value:     float32(ageSeconds),
			Tags:      tags,
		})
	}

	// Counts per STATUS.
	for status, count := range statusCounts {
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.scheduled_job.count_by_status",
			Timestamp: ts,
			Value:     float32(count),
			Tags: []tags.Tag{
				hostTag,
				{Key: "status", Value: status},
			},
		})
	}

	// Total aggregate.
	points = append(points, datapoint.DataPoint{
		Name:      "ibmi.scheduled_job.total",
		Timestamp: ts,
		Value:     float32(len(res.Rows)),
		Tags:      []tags.Tag{hostTag},
	})

	if len(points) == 0 {
		return nil, fmt.Errorf("scheduled_job produced no datapoints")
	}
	return points, nil
}

// ibmiTimestampShortLayout matches the "2025-12-07 06:00:00" format
// returned by LAST_SUCCESSFUL_SUBMISSION_TIMESTAMP on IBM i 7.5 —
// seconds precision, no microseconds.
const ibmiTimestampShortLayout = "2006-01-02 15:04:05"
