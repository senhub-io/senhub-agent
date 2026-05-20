package ibmi

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// jobQueueCollector reads per-JOBQ state from QSYS2.JOB_QUEUE_INFO.
// Only queues with non-zero content are emitted as per-queue metrics
// (to keep cardinality reasonable) but the collector always emits
// global aggregates so dashboards have continuous time series.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-job-queue-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
type jobQueueCollector struct{}

func (jobQueueCollector) Name() string  { return "job_queue" }
func (jobQueueCollector) IsEvent() bool { return false }

func (jobQueueCollector) SQL() string {
	return "SELECT JOB_QUEUE_NAME, JOB_QUEUE_LIBRARY, JOB_QUEUE_STATUS, NUMBER_OF_JOBS, SUBSYSTEM_NAME, ACTIVE_JOBS, HELD_JOBS, RELEASED_JOBS, SCHEDULED_JOBS FROM QSYS2.JOB_QUEUE_INFO WHERE NUMBER_OF_JOBS > 0"
}

func (jobQueueCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	// Zero non-empty JOBQs is a valid state on an idle LPAR. We
	// still emit the aggregate datapoints so the series stays
	// continuous.
	idx := columnIndex(res.Columns)
	hostTag := tags.Tag{Key: "host", Value: host}

	var (
		totalQueues    int
		totalJobs      float64
		totalHeld      float64
		totalReleased  float64
		totalScheduled float64
		totalActive    float64
	)
	points := make([]datapoint.DataPoint, 0, len(res.Rows)*4+5)

	for _, row := range res.Rows {
		name := trimmedCell(row, idx, "JOB_QUEUE_NAME")
		library := trimmedCell(row, idx, "JOB_QUEUE_LIBRARY")
		if name == "" {
			continue
		}
		status := trimmedCell(row, idx, "JOB_QUEUE_STATUS")
		subsystem := trimmedCell(row, idx, "SUBSYSTEM_NAME")
		if subsystem == "" {
			subsystem = "<none>"
		}
		totalQueues++

		tags := []tags.Tag{
			hostTag,
			{Key: "queue_name", Value: name},
			{Key: "queue_library", Value: library},
			{Key: "queue_status", Value: status},
			{Key: "subsystem", Value: subsystem},
		}

		metrics := []struct {
			column string
			metric string
			acc    *float64
		}{
			{"NUMBER_OF_JOBS", "ibmi.job_queue.jobs_total", &totalJobs},
			{"ACTIVE_JOBS", "ibmi.job_queue.active_jobs", &totalActive},
			{"HELD_JOBS", "ibmi.job_queue.held_jobs", &totalHeld},
			{"RELEASED_JOBS", "ibmi.job_queue.released_jobs", &totalReleased},
			{"SCHEDULED_JOBS", "ibmi.job_queue.scheduled_jobs", &totalScheduled},
		}
		for _, m := range metrics {
			v, ok := parseFloatCell(row, idx, m.column)
			if !ok {
				continue
			}
			*m.acc += v
			points = append(points, datapoint.DataPoint{
				Name:      m.metric,
				Timestamp: ts,
				Value:     float32(v),
				Tags:      tags,
			})
		}
	}

	// Aggregates emitted unconditionally so dashboards have a
	// continuous signal even when every queue is empty.
	aggregates := []struct {
		name  string
		value float64
	}{
		{"ibmi.job_queue.nonempty_total", float64(totalQueues)},
		{"ibmi.job_queue.jobs_sum", totalJobs},
		{"ibmi.job_queue.active_sum", totalActive},
		{"ibmi.job_queue.held_sum", totalHeld},
		{"ibmi.job_queue.released_sum", totalReleased},
		{"ibmi.job_queue.scheduled_sum", totalScheduled},
	}
	for _, a := range aggregates {
		points = append(points, datapoint.DataPoint{
			Name:      a.name,
			Timestamp: ts,
			Value:     float32(a.value),
			Tags:      []tags.Tag{hostTag},
		})
	}

	if len(points) == 0 {
		return nil, fmt.Errorf("job_queue produced no datapoints")
	}
	return points, nil
}
