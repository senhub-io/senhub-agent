package ibmi

import (
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// msgwJobCollector is a stateful event collector that detects jobs
// entering (and still stuck in) the MSGW state — "message wait",
// meaning the job is blocked on an unanswered inquiry message to
// QSYSOPR. It is the number-one operational alert an IBM i admin
// wants to see: a job stuck on MSGW can hold a whole workload.
//
// State: a set of INTERNAL_JOB_IDs observed in MSGW on the previous
// cycle. On each cycle:
//   - every job CURRENTLY in MSGW is emitted as an event DataPoint
//     (so the downstream log stream sees a continuous signal, even
//     if the job has been stuck for 5 cycles)
//   - the set of tracked IDs is refreshed so jobs that left MSGW
//     stop being emitted
//
// We deliberately don't emit "exit MSGW" events — the absence of new
// events for a given internal_job_id is already a signal of resolution,
// and emitting exit events doubles the event volume for no real gain.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-active-job-info
type msgwJobCollector struct {
	mu      sync.Mutex
	knownAt map[string]time.Time // internal_job_id → first time seen in MSGW
}

func newMsgwJobCollector() *msgwJobCollector {
	return &msgwJobCollector{knownAt: make(map[string]time.Time)}
}

func (c *msgwJobCollector) Name() string  { return "msgw_job" }
func (c *msgwJobCollector) IsEvent() bool { return true }

func (c *msgwJobCollector) SQL() string {
	return "SELECT INTERNAL_JOB_ID, JOB_NAME, JOB_USER, SUBSYSTEM, JOB_TYPE, JOB_STATUS, CPU_TIME, TEMPORARY_STORAGE FROM TABLE(QSYS2.ACTIVE_JOB_INFO()) AS X WHERE JOB_STATUS = 'MSGW'"
}

func (c *msgwJobCollector) Parse(res *bridge.Result, host string, ts time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	points := make([]datapoint.DataPoint, 0, len(res.Rows))

	c.mu.Lock()
	defer c.mu.Unlock()

	// Rebuild the tracked set from scratch on each cycle: anything
	// still in MSGW is re-added, anything that left is forgotten.
	next := make(map[string]time.Time, len(res.Rows))

	for _, row := range res.Rows {
		internalID := trimmedCell(row, idx, "INTERNAL_JOB_ID")
		if internalID == "" {
			continue
		}
		jobName := trimmedCell(row, idx, "JOB_NAME")
		jobUser := trimmedCell(row, idx, "JOB_USER")
		subsystem := trimmedCell(row, idx, "SUBSYSTEM")
		if subsystem == "" {
			subsystem = "<system>"
		}
		jobType := trimmedCell(row, idx, "JOB_TYPE")

		firstSeen, existed := c.knownAt[internalID]
		if !existed {
			firstSeen = ts
		}
		next[internalID] = firstSeen
		stuckForSeconds := ts.Sub(firstSeen).Seconds()

		// severity "50" is an arbitrary choice signalling
		// operational-level attention; the event strategy
		// formatter only validates non-emptiness.
		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.msgw_job.event",
			Timestamp: ts,
			Value:     float32(stuckForSeconds),
			Tags: []tags.Tag{
				{Key: "host", Value: host},
				{Key: "severity", Value: "50"},
				{Key: "message", Value: "job waiting on inquiry (MSGW)"},
				{Key: "job_name", Value: jobName},
				{Key: "job_user", Value: jobUser},
				{Key: "subsystem", Value: subsystem},
				{Key: "job_type", Value: jobType},
				{Key: "internal_job_id", Value: internalID},
				{Key: "state", Value: "MSGW"},
			},
		})
	}

	c.knownAt = next
	return points, nil
}
