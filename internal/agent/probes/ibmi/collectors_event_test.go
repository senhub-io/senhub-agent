package ibmi

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
)

// These tests exercise the stateful event collectors in isolation:
// watermark seeding, advancement, duplicate rejection on subsequent
// cycles, and null/unparseable field handling. They never hit a real
// bridge — the collector's SQL() is passed through to a fake executor
// in the ibmiProbe-level tests.

// newMessageQueueRow builds a synthetic QSYS2.MESSAGE_QUEUE_INFO row
// matching the column order emitted by messageQueueCollector.SQL().
func newMessageQueueRow(ts, sev, id, msg string) []*string {
	return []*string{
		strPtr(id),                 // MESSAGE_ID
		strPtr("INFORMATIONAL"),    // MESSAGE_TYPE
		strPtr(msg),                // MESSAGE_TEXT
		strPtr(sev),                // SEVERITY
		strPtr(ts),                 // MESSAGE_TIMESTAMP
		strPtr("QSYS"),             // FROM_USER
		strPtr("000000/QSYS/SCPF"), // FROM_JOB
	}
}

func messageQueueColumns() []string {
	return []string{
		"MESSAGE_ID", "MESSAGE_TYPE", "MESSAGE_TEXT", "SEVERITY",
		"MESSAGE_TIMESTAMP", "FROM_USER", "FROM_JOB",
	}
}

func TestMessageQueueCollector_SeedsWatermarkOnFirstSQL(t *testing.T) {
	c := newMessageQueueCollector()
	before := time.Now().UTC()
	sql := c.SQL()
	after := time.Now().UTC()

	if c.watermark.Before(before) || c.watermark.After(after) {
		t.Errorf("watermark not seeded inside [%v, %v]: %v", before, after, c.watermark)
	}
	// The generated SQL must carry the seeded timestamp and the
	// expected QSYSOPR filter, otherwise the caller would either
	// replay the entire history or hit the wrong queue.
	if !strings.Contains(sql, "QSYSOPR") {
		t.Errorf("SQL missing QSYSOPR filter: %s", sql)
	}
	if !strings.Contains(sql, c.watermark.Format(ibmiTimestampLayout)) {
		t.Errorf("SQL does not embed the seeded watermark: %s", sql)
	}
}

func TestMessageQueueCollector_AdvancesWatermark(t *testing.T) {
	c := newMessageQueueCollector()
	// Force an old watermark so the synthetic rows look "new".
	c.watermark = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	res := &bridge.Result{
		Columns: messageQueueColumns(),
		Rows: [][]*string{
			newMessageQueueRow("2026-04-15 10:00:00.000000", "20", "CPI0001", "first"),
			newMessageQueueRow("2026-04-15 10:05:00.000000", "50", "CPF0002", "second"),
			newMessageQueueRow("2026-04-15 10:03:00.000000", "10", "CPI0003", "out-of-order"),
		},
	}

	points, err := c.Parse(res, "pub400.com", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 events, got %d", len(points))
	}

	// Watermark should now reflect the newest row, not the last one
	// in the slice — the iteration must look at every timestamp.
	// Parsed timestamps live in time.Local, so the expected value is
	// constructed with the same location.
	want := time.Date(2026, 4, 15, 10, 5, 0, 0, time.Local)
	if !c.watermark.Equal(want) {
		t.Errorf("watermark: got %v, want %v", c.watermark, want)
	}

	// Every event carries the mandatory event-strategy tags.
	for _, dp := range points {
		for _, k := range []string{"host", "severity", "message", "message_id"} {
			found := false
			for _, tg := range dp.Tags {
				if tg.Key == k && tg.Value != "" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: missing required tag %q in %#v", dp.Name, k, dp.Tags)
			}
		}
	}
}

func TestMessageQueueCollector_UnparseableTimestampIsSkipped(t *testing.T) {
	c := newMessageQueueCollector()
	c.watermark = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	res := &bridge.Result{
		Columns: messageQueueColumns(),
		Rows: [][]*string{
			newMessageQueueRow("garbage", "10", "CPI0001", "bad ts"),
			newMessageQueueRow("2026-04-15 10:00:00.000000", "20", "CPI0002", "good"),
		},
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 event (the good one), got %d", len(points))
	}
}

func TestMessageQueueCollector_EmptyResultIsSuccess(t *testing.T) {
	c := newMessageQueueCollector()
	original := c.watermark
	c.watermark = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = original

	res := &bridge.Result{Columns: messageQueueColumns(), Rows: nil}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("empty result should not error: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("expected 0 events, got %d", len(points))
	}
}

// History log — same test battery, mapped onto its column layout.

func newHistoryLogRow(ts, sev, id, msg string) []*string {
	return []*string{
		strPtr(id),                 // MESSAGE_ID
		strPtr("INFORMATIONAL"),    // MESSAGE_TYPE
		strPtr(msg),                // MESSAGE_TEXT
		strPtr(sev),                // SEVERITY
		strPtr(ts),                 // MESSAGE_TIMESTAMP
		strPtr("QUSER"),            // FROM_USER
		strPtr("000000/QUSER/JOB"), // FROM_JOB
		strPtr("QPGM"),             // FROM_PROGRAM
	}
}

func historyLogColumns() []string {
	return []string{
		"MESSAGE_ID", "MESSAGE_TYPE", "MESSAGE_TEXT", "SEVERITY",
		"MESSAGE_TIMESTAMP", "FROM_USER", "FROM_JOB", "FROM_PROGRAM",
	}
}

func TestHistoryLogCollector_WatermarkAdvancesPastBoundary(t *testing.T) {
	c := newHistoryLogCollector()
	c.watermark = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	res := &bridge.Result{
		Columns: historyLogColumns(),
		Rows: [][]*string{
			newHistoryLogRow("2026-04-15 10:00:00.000000", "0", "CPF1124", "start job"),
			newHistoryLogRow("2026-04-15 10:00:00.500000", "30", "CPF1164", "end job"),
		},
	}
	if _, err := c.Parse(res, "h", time.Now()); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Watermark must be strictly greater than the newest row — the
	// history log TABLE function has an inclusive start bound, and
	// without the +1µs bump the next cycle would re-emit the row.
	// Parsed timestamps live in time.Local.
	want := time.Date(2026, 4, 15, 10, 0, 0, 500000000+1000, time.Local)
	if !c.watermark.Equal(want) {
		t.Errorf("watermark not bumped past boundary: got %v want %v", c.watermark, want)
	}
}

func TestHistoryLogCollector_ValueIsSeverityFloat(t *testing.T) {
	c := newHistoryLogCollector()
	c.watermark = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	res := &bridge.Result{
		Columns: historyLogColumns(),
		Rows: [][]*string{
			newHistoryLogRow("2026-04-15 10:00:00.000000", "40", "CPF9999", "warn"),
		},
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 event, got %d", len(points))
	}
	if points[0].Value != 40 {
		t.Errorf("Value: want 40, got %v", points[0].Value)
	}
	if points[0].Name != "ibmi.history_log.event" {
		t.Errorf("Name: got %q", points[0].Name)
	}
}
