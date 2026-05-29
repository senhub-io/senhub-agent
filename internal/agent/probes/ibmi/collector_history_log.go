package ibmi

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// historyLogCollector polls the QHST (history log) via the table
// function QSYS2.HISTORY_LOG_INFO(start, end). Unlike MESSAGE_QUEUE_INFO
// the history log requires an explicit time window on every call, so
// the watermark here is passed directly as the start bound.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-history-log-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
// Notably HISTORY_LOG_INFO carries FROM_JOB_NAME / FROM_JOB_USER /
// FROM_JOB_NUMBER as separate columns in addition to the composite
// FROM_JOB, plus SYSLOG_* columns we deliberately ignore (matching the
// project decision to stick with the native IBM i severity scale).
//
// Event DataPoint shape:
//
//	Name      = "ibmi.history_log.event"
//	Timestamp = MESSAGE_TIMESTAMP of the row
//	Value     = SEVERITY (0–99 as a float32)
//	Tags      = host, severity, message, message_id, message_type,
//	            from_user, from_job, from_program
type historyLogCollector struct {
	mu        sync.Mutex
	watermark time.Time // advanced to max(MESSAGE_TIMESTAMP)+1µs on each Parse
}

func newHistoryLogCollector() *historyLogCollector {
	return &historyLogCollector{}
}

func (c *historyLogCollector) Name() string  { return "history_log" }
func (c *historyLogCollector) IsEvent() bool { return true }

// SQL builds the table-function call with the current watermark as the
// start of the time window. On first call the watermark is seeded to
// now() to avoid replaying the entire QHST backlog at startup. We
// always use CURRENT_TIMESTAMP as the end bound so the window keeps
// expanding until the probe catches up.
func (c *historyLogCollector) SQL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watermark.IsZero() {
		c.watermark = time.Now()
	}
	start := c.watermark.Format(ibmiTimestampLayout)
	return fmt.Sprintf(
		"SELECT MESSAGE_ID, MESSAGE_TYPE, MESSAGE_TEXT, SEVERITY, MESSAGE_TIMESTAMP, FROM_USER, FROM_JOB, FROM_PROGRAM FROM TABLE(QSYS2.HISTORY_LOG_INFO(TIMESTAMP('%s'), CURRENT_TIMESTAMP)) AS X ORDER BY MESSAGE_TIMESTAMP FETCH FIRST 200 ROWS ONLY",
		start,
	)
}

func (c *historyLogCollector) Parse(res *bridge.Result, host string, _ time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	points := make([]datapoint.DataPoint, 0, len(res.Rows))
	var newWatermark time.Time

	for _, row := range res.Rows {
		rawTs, present, _ := requireCell(row, idx, "MESSAGE_TIMESTAMP")
		if !present {
			continue
		}
		// Same timezone handling as messageQueueCollector: server
		// local time, parsed as time.Local to keep the offset
		// intact on the way out to JSON.
		msgTs, err := time.ParseInLocation(ibmiTimestampLayout, rawTs, time.Local)
		if err != nil {
			continue
		}
		if msgTs.After(newWatermark) {
			newWatermark = msgTs
		}

		msgID, _, _ := requireCell(row, idx, "MESSAGE_ID")
		msgType, _, _ := requireCell(row, idx, "MESSAGE_TYPE")
		msgText, _, _ := requireCell(row, idx, "MESSAGE_TEXT")
		severityRaw, _, _ := requireCell(row, idx, "SEVERITY")
		fromUser, _, _ := requireCell(row, idx, "FROM_USER")
		fromJob, _, _ := requireCell(row, idx, "FROM_JOB")
		fromProgram, _, _ := requireCell(row, idx, "FROM_PROGRAM")

		severity, _ := strconv.ParseFloat(strings.TrimSpace(severityRaw), 64)

		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.history_log.event",
			Timestamp: msgTs,
			Value:     float32(severity),
			Tags: []tags.Tag{
				{Key: "host", Value: host},
				{Key: "severity", Value: strings.TrimSpace(severityRaw)},
				{Key: "message", Value: strings.TrimSpace(msgText)},
				{Key: "message_id", Value: strings.TrimSpace(msgID)},
				{Key: "message_type", Value: strings.TrimSpace(msgType)},
				{Key: "from_user", Value: strings.TrimSpace(fromUser)},
				{Key: "from_job", Value: strings.TrimSpace(fromJob)},
				{Key: "from_program", Value: strings.TrimSpace(fromProgram)},
			},
		})
	}

	// Advance watermark past the newest message by a single
	// microsecond. IBM i TIMESTAMP has microsecond resolution and
	// HISTORY_LOG_INFO's start bound is inclusive, so without the
	// +1µs bump we would re-emit the boundary row on every cycle.
	if !newWatermark.IsZero() {
		c.mu.Lock()
		c.watermark = newWatermark.Add(time.Microsecond)
		c.mu.Unlock()
	}

	return points, nil
}
