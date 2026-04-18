package ibmi

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// auditJournalCollector reads security-relevant events from the IBM i
// QAUDJRN audit journal via QSYS2.DISPLAY_JOURNAL. It is a stateful
// event collector with a watermark on JOURNAL_ENTRY_TIMESTAMP — same
// pattern as historyLogCollector but scoped to QAUDJRN and filtered
// on a curated list of entry types (AF authority failure, PW password
// failure, SV system value change, CA authority change, ...).
//
// **Not in defaultCollectors() yet.** Reading QAUDJRN requires *ALLOBJ
// or *AUDIT special authority; the PGMR profile on PUB400 does not
// have either, so activating this on the sandbox would only generate
// failure_total increments without producing events. It is wired in
// code for production deployments that grant the right authorities —
// in Sprint 8 it will be enabled/disabled through the config layer
// alongside the other opt-in collectors.
//
// Ref: https://www.ibm.com/docs/en/i/7.5?topic=services-display-journal
type auditJournalCollector struct {
	mu        sync.Mutex
	watermark time.Time
}

// newAuditJournalCollector returns a fresh collector pointed at the
// system audit journal QSYS/QAUDJRN. Other journals could be targeted
// by varying the constructor arguments in the future.
//
// newAuditJournalCollector returns an audit_journal collector that is
// registered in allKnownCollectors() but not in defaultCollectors().
// Activate it via `enabled_collectors: [audit_journal, ...]` in the
// probe configuration once the target LPAR has granted *ALLOBJ or
// *AUDIT to the bridge user profile.
func newAuditJournalCollector() *auditJournalCollector {
	return &auditJournalCollector{}
}

func (c *auditJournalCollector) Name() string { return "audit_journal" }

func (c *auditJournalCollector) IsEvent() bool { return true }

// auditEntryTypes is the curated set of QAUDJRN entry types we care
// about. Including everything would flood the log sink with routine
// entries; this list captures the ones a security auditor typically
// reviews on a weekly basis.
//

var auditEntryTypes = []string{
	"AF", // Authority Failure
	"CA", // Authority Change
	"CO", // Object Create
	"CP", // User Profile Change
	"DO", // Object Delete
	"OR", // Object Restore
	"OW", // Object Ownership Change
	"PA", // Program Adoption
	"PW", // Password Failure
	"SV", // System Value Change
	"ZC", // Object Change (data integrity)
}

func (c *auditJournalCollector) SQL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watermark.IsZero() {
		c.watermark = time.Now()
	}
	start := c.watermark.Format(ibmiTimestampLayout)

	quoted := make([]string, 0, len(auditEntryTypes))
	for _, t := range auditEntryTypes {
		quoted = append(quoted, "'"+t+"'")
	}

	return fmt.Sprintf(
		"SELECT JOURNAL_ENTRY_TIMESTAMP, JOURNAL_ENTRY_TYPE, OBJECT, OBJECT_LIBRARY, OBJECT_TYPE, JOB_NAME, CURRENT_USER, ENTRY_DATA FROM TABLE(QSYS2.DISPLAY_JOURNAL('QSYS','QAUDJRN', JOURNAL_ENTRY_TYPES => '%s', STARTING_TIMESTAMP => TIMESTAMP('%s'))) AS X ORDER BY JOURNAL_ENTRY_TIMESTAMP FETCH FIRST 200 ROWS ONLY",
		strings.Join(quoted, ","),
		start,
	)
}

func (c *auditJournalCollector) Parse(res *bridge.Result, host string, _ time.Time) ([]datapoint.DataPoint, error) {
	idx := columnIndex(res.Columns)
	points := make([]datapoint.DataPoint, 0, len(res.Rows))
	var newWatermark time.Time

	for _, row := range res.Rows {
		rawTs, present, _ := requireCell(row, idx, "JOURNAL_ENTRY_TIMESTAMP")
		if !present {
			continue
		}
		msgTs, err := time.ParseInLocation(ibmiTimestampLayout, rawTs, time.Local)
		if err != nil {
			continue
		}
		if msgTs.After(newWatermark) {
			newWatermark = msgTs
		}

		entryType := trimmedCell(row, idx, "JOURNAL_ENTRY_TYPE")
		object := trimmedCell(row, idx, "OBJECT")
		objectLib := trimmedCell(row, idx, "OBJECT_LIBRARY")
		objectType := trimmedCell(row, idx, "OBJECT_TYPE")
		jobName := trimmedCell(row, idx, "JOB_NAME")
		currentUser := trimmedCell(row, idx, "CURRENT_USER")
		entryData := trimmedCell(row, idx, "ENTRY_DATA")

		// Severity mapping for the event strategy: AF/PW are
		// critical (actual security failures), SV/CA/OW are
		// alert-level (privileged changes), the rest are notices.
		severity := "30"
		switch entryType {
		case "AF", "PW":
			severity = "60"
		case "SV", "CA", "OW":
			severity = "50"
		}

		message := fmt.Sprintf("QAUDJRN %s: user=%s object=%s/%s/%s",
			entryType, currentUser, objectLib, object, objectType)

		points = append(points, datapoint.DataPoint{
			Name:      "ibmi.audit_journal.event",
			Timestamp: msgTs,
			Value:     float32(len(entryData)),
			Tags: []tags.Tag{
				{Key: "host", Value: host},
				{Key: "severity", Value: severity},
				{Key: "message", Value: message},
				{Key: "entry_type", Value: entryType},
				{Key: "object", Value: object},
				{Key: "object_library", Value: objectLib},
				{Key: "object_type", Value: objectType},
				{Key: "job_name", Value: jobName},
				{Key: "current_user", Value: currentUser},
			},
		})
	}

	if !newWatermark.IsZero() {
		c.mu.Lock()
		c.watermark = newWatermark.Add(time.Microsecond)
		c.mu.Unlock()
	}

	return points, nil
}
