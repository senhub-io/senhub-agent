package ibmi

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
)

// Unit tests for Sprint 4 job-related collectors.

func TestActiveJobCollector_AggregatesAndTopN(t *testing.T) {
	c := activeJobCollector{}
	res := &bridge.Result{
		Columns: []string{
			"JOB_NAME", "JOB_USER", "SUBSYSTEM", "JOB_TYPE", "JOB_STATUS",
			"RUN_PRIORITY", "THREAD_COUNT", "TEMPORARY_STORAGE", "CPU_TIME",
			"ELAPSED_CPU_PERCENTAGE", "ELAPSED_CPU_TIME", "TOTAL_DISK_IO_COUNT",
		},
		Rows: [][]*string{
			// Three jobs to exercise every aggregation axis.
			{strPtr("001/QSYS/QZDASOINIT"), strPtr("QUSER"), strPtr("QSERVER"),
				strPtr("BCH"), strPtr("ACTIVE"), strPtr("20"), strPtr("1"),
				strPtr("5"), strPtr("1200"), strPtr("50.0"), strPtr("600"),
				strPtr("1000")},
			{strPtr("002/QSYS/QZDASOINIT"), strPtr("QUSER"), strPtr("QSERVER"),
				strPtr("BCH"), strPtr("ACTIVE"), strPtr("20"), strPtr("1"),
				strPtr("3"), strPtr("800"), strPtr("20.0"), strPtr("300"),
				strPtr("500")},
			{strPtr("003/QSYS/SCPF"), strPtr("QSYS"), nil,
				strPtr("SYS"), strPtr("EVTW"), strPtr("40"), strPtr("1"),
				strPtr("100"), strPtr("20000"), strPtr("0.0"), strPtr("0"),
				strPtr("500000")},
		},
	}

	points, err := c.Parse(res, "pub400.com", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	byName := make(map[string][]float32, 8)
	for _, dp := range points {
		byName[dp.Name] = append(byName[dp.Name], dp.Value)
	}

	// Aggregate sanity: active_total should equal the number of rows
	if len(byName["ibmi.jobs.active_total"]) != 1 || byName["ibmi.jobs.active_total"][0] != 3 {
		t.Errorf("active_total: want [3], got %v", byName["ibmi.jobs.active_total"])
	}

	// Two BCH+ACTIVE, one SYS+EVTW.
	countByStatus := byName["ibmi.jobs.count_by_status"]
	if len(countByStatus) != 2 {
		t.Errorf("count_by_status: expected 2 groups, got %d", len(countByStatus))
	}

	// Per-job details for top-N: 3 rows × 7 metrics = 21 expected
	// per-job datapoints (may be fewer if NULLs trim them).
	perJobMetrics := []string{
		"ibmi.job.elapsed_cpu_percent",
		"ibmi.job.elapsed_cpu_ms",
		"ibmi.job.cpu_time_ms",
		"ibmi.job.temp_storage_mb",
		"ibmi.job.total_disk_io",
		"ibmi.job.thread_count",
		"ibmi.job.run_priority",
	}
	for _, name := range perJobMetrics {
		if len(byName[name]) != 3 {
			t.Errorf("%s: expected 3 per-job datapoints, got %d", name, len(byName[name]))
		}
	}

	// cap_hit should be 0 since we only have 3 rows, well under 200.
	if byName["ibmi.jobs.topn_cap_hit"][0] != 0 {
		t.Errorf("topn_cap_hit: expected 0, got %v", byName["ibmi.jobs.topn_cap_hit"][0])
	}
}

func TestActiveJobCollector_SystemJobsGetLabelFallback(t *testing.T) {
	c := activeJobCollector{}
	res := &bridge.Result{
		Columns: []string{"JOB_NAME", "JOB_USER", "SUBSYSTEM", "JOB_TYPE", "JOB_STATUS",
			"RUN_PRIORITY", "THREAD_COUNT", "TEMPORARY_STORAGE", "CPU_TIME",
			"ELAPSED_CPU_PERCENTAGE", "ELAPSED_CPU_TIME", "TOTAL_DISK_IO_COUNT"},
		Rows: [][]*string{
			{strPtr("000/QSYS/SCPF"), strPtr("QSYS"), nil, strPtr("SYS"),
				strPtr("EVTW"), strPtr("0"), strPtr("1"), strPtr("100"),
				strPtr("20000"), strPtr("0.00"), strPtr("0"), strPtr("500000")},
		},
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The <system> fallback must be present on the per-subsystem
	// aggregate so the system jobs aren't silently dropped or pushed
	// under the empty-string tag.
	foundSystemFallback := false
	for _, dp := range points {
		if dp.Name == "ibmi.jobs.count_by_subsystem" {
			for _, tg := range dp.Tags {
				if tg.Key == "subsystem" && tg.Value == "<system>" {
					foundSystemFallback = true
				}
			}
		}
	}
	if !foundSystemFallback {
		t.Error("missing <system> subsystem fallback on null SUBSYSTEM row")
	}
}

func TestJobQueueCollector_EmitsAggregatesEvenWhenEmpty(t *testing.T) {
	c := jobQueueCollector{}
	res := &bridge.Result{
		Columns: []string{
			"JOB_QUEUE_NAME", "JOB_QUEUE_LIBRARY", "JOB_QUEUE_STATUS",
			"NUMBER_OF_JOBS", "SUBSYSTEM_NAME", "ACTIVE_JOBS", "HELD_JOBS",
			"RELEASED_JOBS", "SCHEDULED_JOBS",
		},
		Rows: nil,
	}
	points, err := c.Parse(res, "h", time.Now())
	if err != nil {
		t.Fatalf("empty rows should still produce aggregates, got %v", err)
	}
	wantAggregates := map[string]bool{
		"ibmi.job_queue.nonempty_total": false,
		"ibmi.job_queue.jobs_sum":       false,
	}
	for _, dp := range points {
		if _, ok := wantAggregates[dp.Name]; ok {
			wantAggregates[dp.Name] = true
		}
	}
	for name, seen := range wantAggregates {
		if !seen {
			t.Errorf("missing expected aggregate %s", name)
		}
	}
}

func TestScheduledJobCollector_LastRunAgeFromTimestamp(t *testing.T) {
	c := scheduledJobCollector{}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{
			"SCHEDULED_JOB_NAME", "STATUS", "FREQUENCY",
			"NEXT_SUBMISSION_DATE", "LAST_SUCCESSFUL_SUBMISSION_TIMESTAMP",
			"LAST_ATTEMPTED_SUBMISSION_STATUS",
		},
		Rows: [][]*string{
			{strPtr("NIGHTLY"), strPtr("SCD"), strPtr("*WEEKLY"),
				strPtr("26-04-16"), strPtr("2026-04-15 11:00:00"), strPtr("COMPLETED")},
			{strPtr("BROKEN"), strPtr("HLD"), strPtr("*ONCE"),
				nil, nil, nil}, // never run successfully
		},
	}
	points, err := c.Parse(res, "h", now)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var nightlyAge, brokenAge float32 = -99, -99
	for _, dp := range points {
		if dp.Name != "ibmi.scheduled_job.last_run_age_seconds" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "job_name" && tg.Value == "NIGHTLY" {
				nightlyAge = dp.Value
			}
			if tg.Key == "job_name" && tg.Value == "BROKEN" {
				brokenAge = dp.Value
			}
		}
	}
	// Nightly ran 1 hour ago → 3600s
	if nightlyAge < 3599 || nightlyAge > 3601 {
		t.Errorf("NIGHTLY last_run_age: want ~3600, got %v", nightlyAge)
	}
	// Broken never ran → sentinel -1
	if brokenAge != -1 {
		t.Errorf("BROKEN last_run_age: want -1 sentinel, got %v", brokenAge)
	}
}

func TestMsgwJobCollector_TracksJobsAcrossCycles(t *testing.T) {
	c := newMsgwJobCollector()

	// Cycle 1: one job in MSGW.
	cycle1 := &bridge.Result{
		Columns: []string{
			"INTERNAL_JOB_ID", "JOB_NAME", "JOB_USER", "SUBSYSTEM",
			"JOB_TYPE", "JOB_STATUS", "CPU_TIME", "TEMPORARY_STORAGE",
		},
		Rows: [][]*string{
			{strPtr("ID1"), strPtr("JOBA"), strPtr("USER"), strPtr("QBATCH"),
				strPtr("BCH"), strPtr("MSGW"), strPtr("100"), strPtr("5")},
		},
	}
	t0 := time.Date(2026, 4, 15, 12, 0, 0, 0, time.Local)
	points, err := c.Parse(cycle1, "h", t0)
	if err != nil {
		t.Fatalf("cycle 1 Parse: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("cycle 1 expected 1 event, got %d", len(points))
	}
	if points[0].Value != 0 { // first seen, 0 seconds stuck
		t.Errorf("cycle 1 stuckFor: want 0, got %v", points[0].Value)
	}

	// Cycle 2 — same job still in MSGW, 30s later, plus a new one.
	cycle2 := &bridge.Result{
		Columns: cycle1.Columns,
		Rows: [][]*string{
			{strPtr("ID1"), strPtr("JOBA"), strPtr("USER"), strPtr("QBATCH"),
				strPtr("BCH"), strPtr("MSGW"), strPtr("105"), strPtr("5")},
			{strPtr("ID2"), strPtr("JOBB"), strPtr("USER"), strPtr("QBATCH"),
				strPtr("BCH"), strPtr("MSGW"), strPtr("1"), strPtr("1")},
		},
	}
	t1 := t0.Add(30 * time.Second)
	points, err = c.Parse(cycle2, "h", t1)
	if err != nil {
		t.Fatalf("cycle 2 Parse: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("cycle 2 expected 2 events, got %d", len(points))
	}

	// Value semantics: JOBA (already tracked) should report 30s stuck,
	// JOBB (new) should report 0s.
	var jobA, jobB float32 = -1, -1
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "job_name" && tg.Value == "JOBA" {
				jobA = dp.Value
			}
			if tg.Key == "job_name" && tg.Value == "JOBB" {
				jobB = dp.Value
			}
		}
	}
	if jobA != 30 {
		t.Errorf("JOBA stuckFor: want 30, got %v", jobA)
	}
	if jobB != 0 {
		t.Errorf("JOBB stuckFor: want 0, got %v", jobB)
	}

	// Cycle 3 — JOBA left MSGW, only JOBB remains. The state set
	// must drop JOBA; if it doesn't, a future cycle that brings back
	// JOBA would report the wrong stuckFor.
	cycle3 := &bridge.Result{
		Columns: cycle1.Columns,
		Rows: [][]*string{
			{strPtr("ID2"), strPtr("JOBB"), strPtr("USER"), strPtr("QBATCH"),
				strPtr("BCH"), strPtr("MSGW"), strPtr("2"), strPtr("1")},
		},
	}
	t2 := t0.Add(45 * time.Second)
	if _, err := c.Parse(cycle3, "h", t2); err != nil {
		t.Fatalf("cycle 3 Parse: %v", err)
	}
	if _, stillTracked := c.knownAt["ID1"]; stillTracked {
		t.Error("JOBA (ID1) should have been evicted from state after cycle 3")
	}
}
