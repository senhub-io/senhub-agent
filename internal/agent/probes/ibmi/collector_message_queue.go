package ibmi

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// messageQueueCollector polls QSYS2.MESSAGE_QUEUE_INFO for new entries
// in QSYSOPR (the operator message queue) and turns each row into an
// event DataPoint. It is the first stateful collector: between calls
// it keeps a watermark on MESSAGE_TIMESTAMP so subsequent cycles only
// fetch messages that arrived since the last successful Parse.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-message-queue-info
//
// Columns verified on PUB400 (IBM i 7.5, 2026-04-15) via tools/sql-inspect.
// The message queue timestamp format returned by IBM i is
// "2026-04-12 10:11:58.076444" — matching Go's reference layout
// "2006-01-02 15:04:05.000000".
//
// Event DataPoint shape (consumed by the senhub-agent event strategy):
//
//	Name      = "ibmi.message_queue.event"
//	Timestamp = MESSAGE_TIMESTAMP (the actual time of the message,
//	            *not* the collection cycle timestamp)
//	Value     = SEVERITY (0–99 as a float32)
//	Tags      = host, severity (string), message, message_id,
//	            message_type, from_user, from_job, queue_library,
//	            queue_name
//
// Matthieu's design note: we deliberately do not map IBM i severity
// (0–99) onto syslog severity (0–7). The senhub-agent event strategy
// formatter will see whatever we put in "severity" and route the data
// through — downstream log sinks can interpret the IBM i scale
// directly and retain its full resolution.
type messageQueueCollector struct {
	mu            sync.Mutex
	watermark     time.Time // MESSAGE_TIMESTAMP of the newest row seen
	queueLib      string
	queueName     string
	minSeverity   int // inclusive lower bound on IBM i SEVERITY (0-99)
	collectorName string
}

// newMessageQueueCollector returns a collector that polls the classic
// operator queue QSYS/QSYSOPR. The resulting collector keeps the stable
// identifier "message_queue" so pre-existing dashboards and health
// metrics continue to work.
//
// Additional queues (QSYSMSG, a named application queue, etc.) are
// instantiated via newMessageQueueCollectorFor — they get a distinct
// Name() so the probe's health book-keeping tracks them independently.
//
// Severity threshold default: 0 (capture every message). Production
// deployments typically raise this to 30 or 50 to filter the INFO-level
// noise. Lowering or raising is a configuration choice.
func newMessageQueueCollector() *messageQueueCollector {
	return &messageQueueCollector{
		queueLib:      "QSYS",
		queueName:     "QSYSOPR",
		minSeverity:   0,
		collectorName: "message_queue",
	}
}

// newMessageQueueCollectorFor returns a collector scoped to the given
// (library, name) queue with an IBM i severity floor. The collector's
// reported Name() is "message_queue_<lowercase(name)>" for any queue
// other than QSYSOPR — QSYSOPR keeps the legacy "message_queue" name.
func newMessageQueueCollectorFor(library, name string, minSeverity int) *messageQueueCollector {
	lib := strings.ToUpper(strings.TrimSpace(library))
	qn := strings.ToUpper(strings.TrimSpace(name))
	collectorName := "message_queue"
	if qn != "QSYSOPR" {
		collectorName = "message_queue_" + strings.ToLower(qn)
	}
	return &messageQueueCollector{
		queueLib:      lib,
		queueName:     qn,
		minSeverity:   minSeverity,
		collectorName: collectorName,
	}
}

func (c *messageQueueCollector) Name() string  { return c.collectorName }
func (c *messageQueueCollector) IsEvent() bool { return true }

// ibmiTimestampLayout is the Go layout that matches the IBM i TIMESTAMP
// string representation used by jt400 for MESSAGE_QUEUE_INFO and
// HISTORY_LOG_INFO. IBM i renders these columns in the *server local
// timezone* without any offset, so both sides of the watermark filter
// must agree on that timezone. We use time.Local on the client — an
// explicit assumption that the senhub4i probe runs in the same
// timezone as the target LPAR. For cross-timezone deployments we'll
// need an explicit ibmi_timezone config option (deferred to a later
// sprint).
const ibmiTimestampLayout = "2006-01-02 15:04:05.000000"

// SQL builds a watermark-filtered query. On the very first call the
// watermark is seeded to now() (local) so a fresh probe startup does
// not flood the downstream log sink with the entire QSYSOPR history.
// The downside is that we miss any messages that landed between
// process start and the first SQL() call — an acceptable trade-off
// for a poll-based event collector that cares about novelty.
func (c *messageQueueCollector) SQL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watermark.IsZero() {
		c.watermark = time.Now()
	}
	ts := c.watermark.Format(ibmiTimestampLayout)
	return fmt.Sprintf(
		"SELECT MESSAGE_ID, MESSAGE_TYPE, MESSAGE_TEXT, SEVERITY, MESSAGE_TIMESTAMP, FROM_USER, FROM_JOB FROM QSYS2.MESSAGE_QUEUE_INFO WHERE MESSAGE_QUEUE_LIBRARY='%s' AND MESSAGE_QUEUE_NAME='%s' AND MESSAGE_TIMESTAMP > '%s' AND SEVERITY >= %d ORDER BY MESSAGE_TIMESTAMP FETCH FIRST 100 ROWS ONLY",
		c.queueLib, c.queueName, ts, c.minSeverity,
	)
}

func (c *messageQueueCollector) Parse(res *bridge.Result, host string, _ time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	points := make([]datapoint.DataPoint, 0, len(res.Rows))
	var newWatermark time.Time

	for _, row := range res.Rows {
		rawTs, present, _ := requireCell(row, idx, "MESSAGE_TIMESTAMP")
		if !present {
			continue
		}
		// Parse in time.Local so the resulting time.Time keeps the
		// local offset (rather than being silently pinned to UTC,
		// which would skew every downstream comparison and the JSON
		// serialisation by the offset of the server TZ).
		msgTs, err := time.ParseInLocation(ibmiTimestampLayout, rawTs, time.Local)
		if err != nil {
			// Unparseable timestamps get dropped rather than taking
			// the whole collector down. The runCollector loop will
			// still mark the overall call a success if we emit at
			// least one event.
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

		severity, _ := strconv.ParseFloat(strings.TrimSpace(severityRaw), 64)

		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.message_queue.event",
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
				{Key: "queue_library", Value: c.queueLib},
				{Key: "queue_name", Value: c.queueName},
			},
		})
	}

	if !newWatermark.IsZero() {
		c.mu.Lock()
		c.watermark = newWatermark
		c.mu.Unlock()
	}

	// Zero points is a valid outcome — most cycles will have no new
	// operator messages. We never return an error here because the
	// run-collector machinery treats errors as failure counters.
	return points, nil
}
