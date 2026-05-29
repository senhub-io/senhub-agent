package ibmi

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// Tests for the Parse() logic of every non-system-status collector.
// The system_status collector is exercised through the full Collect
// path in ibmiProbe_test.go.

var testTs = time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

func TestASPCollector_Parse(t *testing.T) {
	c := aspCollector{}
	res := &bridge.Result{
		Columns: []string{
			"ASP_NUMBER", "ASP_TYPE", "RDB_NAME",
			"NUMBER_OF_DISK_UNITS", "TOTAL_CAPACITY", "TOTAL_CAPACITY_AVAILABLE",
			"OVERFLOW_STORAGE", "STORAGE_THRESHOLD_PERCENTAGE",
		},
		Rows: [][]*string{{
			strPtr("1"), strPtr("SYSTEM"), strPtr("PUB400"),
			strPtr("21"), strPtr("2104534"), strPtr("719763"),
			strPtr("0"), strPtr("90"),
		}},
	}

	points, err := c.Parse(res, "pub400.com", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// 6 metrics: used_percent (derived) + 5 column-backed
	if len(points) != 6 {
		t.Fatalf("expected 6 datapoints, got %d", len(points))
	}

	// Verify derived used_percent: (2104534 - 719763) / 2104534 * 100 ≈ 65.80
	var usedPercent float32
	for _, dp := range points {
		if dp.Name == "ibmi.asp.used_percent" {
			usedPercent = dp.Value
		}
	}
	if usedPercent < 65.7 || usedPercent > 65.9 {
		t.Errorf("used_percent derivation off: got %v want ~65.8", usedPercent)
	}

	// Every point must carry the asp_number=1, asp_type=SYSTEM tags.
	for _, dp := range points {
		ok1, ok2 := false, false
		for _, tg := range dp.Tags {
			if tg.Key == "asp_number" && tg.Value == "1" {
				ok1 = true
			}
			if tg.Key == "asp_type" && tg.Value == "SYSTEM" {
				ok2 = true
			}
		}
		if !ok1 || !ok2 {
			t.Errorf("%s: missing asp tags %#v", dp.Name, dp.Tags)
		}
	}
}

func TestASPCollector_EmptyRowsReturnsError(t *testing.T) {
	c := aspCollector{}
	_, err := c.Parse(&bridge.Result{Columns: []string{"ASP_NUMBER"}, Rows: nil}, "h", testTs)
	if err == nil {
		t.Fatal("expected error on empty rows")
	}
}

func TestASPCollector_DerivedUsedPercentSkippedOnNullTotal(t *testing.T) {
	c := aspCollector{}
	res := &bridge.Result{
		Columns: []string{
			"ASP_NUMBER", "ASP_TYPE", "RDB_NAME",
			"NUMBER_OF_DISK_UNITS", "TOTAL_CAPACITY", "TOTAL_CAPACITY_AVAILABLE",
			"OVERFLOW_STORAGE", "STORAGE_THRESHOLD_PERCENTAGE",
		},
		Rows: [][]*string{{
			strPtr("1"), strPtr("SYSTEM"), strPtr("PUB400"),
			strPtr("21"), nil, strPtr("719763"),
			strPtr("0"), strPtr("90"),
		}},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, dp := range points {
		if dp.Name == "ibmi.asp.used_percent" {
			t.Error("used_percent should not have been emitted when TOTAL_CAPACITY is NULL")
		}
		if dp.Name == "ibmi.asp.total_capacity_mb" {
			t.Error("total_capacity should not have been emitted when the column is NULL")
		}
	}
}

func TestSubsystemCollector_Parse(t *testing.T) {
	c := subsystemCollector{}
	res := &bridge.Result{
		Columns: []string{
			"SUBSYSTEM_DESCRIPTION", "SUBSYSTEM_DESCRIPTION_LIBRARY",
			"STATUS", "CURRENT_ACTIVE_JOBS", "MAXIMUM_ACTIVE_JOBS",
			"CONTROLLING_SUBSYSTEM",
		},
		Rows: [][]*string{
			{strPtr("QBATCH"), strPtr("QSYS"), strPtr("ACTIVE"), strPtr("5"), strPtr("100"), strPtr("NO")},
			{strPtr("QCTL"), strPtr("QSYS"), strPtr("ACTIVE"), strPtr("1"), nil, strPtr("YES")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// QBATCH should emit both active_jobs_count and max_active_jobs.
	// QCTL should emit only active_jobs_count (MAXIMUM is NULL).
	if len(points) != 3 {
		t.Fatalf("expected 3 points (2 for QBATCH, 1 for QCTL), got %d", len(points))
	}

	var qbatchMax float32 = -1
	var qctlMax float32 = -1
	for _, dp := range points {
		if dp.Name != "ibmi.subsystem.max_active_jobs" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "subsystem" && tg.Value == "QBATCH" {
				qbatchMax = dp.Value
			}
			if tg.Key == "subsystem" && tg.Value == "QCTL" {
				qctlMax = dp.Value
			}
		}
	}
	if qbatchMax != 100 {
		t.Errorf("QBATCH max_active_jobs: want 100, got %v", qbatchMax)
	}
	if qctlMax != -1 {
		t.Errorf("QCTL max_active_jobs: want absent, got %v", qctlMax)
	}
}

func TestSubsystemCollector_EmptyRowsReturnsError(t *testing.T) {
	c := subsystemCollector{}
	_, err := c.Parse(&bridge.Result{Columns: []string{"SUBSYSTEM_DESCRIPTION"}, Rows: nil}, "h", testTs)
	if err == nil {
		t.Fatal("expected error when no active subsystems are returned")
	}
}

func TestMemoryPoolCollector_TrimsPaddedNames(t *testing.T) {
	c := memoryPoolCollector{}
	res := &bridge.Result{
		Columns: []string{
			"SYSTEM_POOL_ID", "POOL_NAME", "CURRENT_SIZE", "RESERVED_SIZE",
			"DEFINED_SIZE", "CURRENT_THREADS", "CURRENT_INELIGIBLE_THREADS",
		},
		Rows: [][]*string{
			{strPtr("1"), strPtr("*MACHINE  "), strPtr("10240.00"), strPtr("3079.17"),
				strPtr("10240.00"), strPtr("269"), strPtr("0")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "pool_name" && tg.Value != "*MACHINE" {
				t.Errorf("pool_name not trimmed: %q", tg.Value)
			}
		}
	}
}

func TestOutputQueueCollector_ZeroQueuesStillEmitsAggregates(t *testing.T) {
	c := outputQueueCollector{}
	res := &bridge.Result{
		Columns: []string{
			"OUTPUT_QUEUE_NAME", "OUTPUT_QUEUE_LIBRARY_NAME",
			"OUTPUT_QUEUE_STATUS", "NUMBER_OF_FILES", "NUMBER_OF_WRITERS",
		},
		Rows: nil,
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Zero rows still produces the 2 aggregate datapoints.
	var seenTotal, seenNonempty bool
	for _, dp := range points {
		if dp.Name == "ibmi.output_queue.spooled_files_total" {
			seenTotal = true
			if dp.Value != 0 {
				t.Errorf("spooled_files_total should be 0, got %v", dp.Value)
			}
		}
		if dp.Name == "ibmi.output_queue.nonempty_total" {
			seenNonempty = true
			if dp.Value != 0 {
				t.Errorf("nonempty_total should be 0, got %v", dp.Value)
			}
		}
	}
	if !seenTotal || !seenNonempty {
		t.Errorf("aggregates missing: total=%v nonempty=%v", seenTotal, seenNonempty)
	}
}

func TestOutputQueueCollector_PerQueueMetrics(t *testing.T) {
	c := outputQueueCollector{}
	res := &bridge.Result{
		Columns: []string{
			"OUTPUT_QUEUE_NAME", "OUTPUT_QUEUE_LIBRARY_NAME",
			"OUTPUT_QUEUE_STATUS", "NUMBER_OF_FILES", "NUMBER_OF_WRITERS",
		},
		Rows: [][]*string{
			{strPtr("MYQ"), strPtr("QGPL"), strPtr("RELEASED"), strPtr("5"), strPtr("1")},
			{strPtr("OTHER"), strPtr("QGPL"), strPtr("HELD"), strPtr("2"), strPtr("0")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var aggregateTotal, aggregateNonempty float32
	perQueueCount := 0
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.output_queue.spooled_files_total":
			aggregateTotal = dp.Value
		case "ibmi.output_queue.nonempty_total":
			aggregateNonempty = dp.Value
		case "ibmi.output_queue.files_count":
			perQueueCount++
		}
	}
	if aggregateTotal != 7 {
		t.Errorf("spooled_files_total: want 7 got %v", aggregateTotal)
	}
	if aggregateNonempty != 2 {
		t.Errorf("nonempty_total: want 2 got %v", aggregateNonempty)
	}
	if perQueueCount != 2 {
		t.Errorf("expected 2 per-queue files_count datapoints, got %d", perQueueCount)
	}
}

func TestSpooledFileCollector_AggregatesAndOldestAge(t *testing.T) {
	c := spooledFileCollector{}
	// Collector ts is 2026-04-17 12:00:00 local; oldest row is 1 hour
	// old, so oldest_age_seconds ≈ 3600.
	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{"STATUS", "FILE_COUNT", "OLDEST_CREATED"},
		Rows: [][]*string{
			{strPtr("RDY"), strPtr("7"), strPtr("2026-04-17 11:00:00.000000")},
			{strPtr("HLD"), strPtr("3"), strPtr("2026-04-17 11:30:00.000000")},
			{strPtr("SAV"), strPtr("1"), nil}, // NULL timestamp tolerated
		},
	}
	points, err := c.Parse(res, "h", ts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var total, oldest float32
	countByStatus := 0
	ageByStatus := 0
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.spooled_file.total":
			total = dp.Value
		case "ibmi.spooled_file.oldest_age_seconds":
			oldest = dp.Value
		case "ibmi.spooled_file.count_by_status":
			countByStatus++
		case "ibmi.spooled_file.oldest_age_seconds_by_status":
			ageByStatus++
		}
	}
	if total != 11 {
		t.Errorf("total: want 11 got %v", total)
	}
	if oldest < 3599 || oldest > 3601 {
		t.Errorf("oldest_age_seconds: want ~3600 got %v", oldest)
	}
	if countByStatus != 3 {
		t.Errorf("count_by_status groups: want 3 got %d", countByStatus)
	}
	// Only two rows carried a timestamp, so only two per-status age
	// datapoints should have been emitted.
	if ageByStatus != 2 {
		t.Errorf("oldest_age_seconds_by_status: want 2 got %d", ageByStatus)
	}
}

func TestUserStorageCollector_QuotaAndUsage(t *testing.T) {
	c := userStorageCollector{}
	res := &bridge.Result{
		Columns: []string{"AUTHORIZATION_NAME", "ASPGRP", "MAXIMUM_STORAGE_ALLOWED", "STORAGE_USED"},
		Rows: [][]*string{
			// Alice: 85% of quota → should flag users_over_80pct
			{strPtr("ALICE"), strPtr("*SYSBAS"), strPtr("1000"), strPtr("850")},
			// Bob: no quota
			{strPtr("BOB"), strPtr("*SYSBAS"), strPtr("*NOMAX"), strPtr("2000")},
			// Carol: 50% of quota
			{strPtr("CAROL"), strPtr("*SYSBAS"), strPtr("1000"), strPtr("500")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var over80, withQuota, totalUsed float32 = -1, -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.user_storage.users_over_80pct":
			over80 = dp.Value
		case "ibmi.user_storage.users_with_quota":
			withQuota = dp.Value
		case "ibmi.user_storage.used_total_kb":
			totalUsed = dp.Value
		}
	}
	if over80 != 1 {
		t.Errorf("users_over_80pct: want 1 got %v", over80)
	}
	if withQuota != 2 {
		t.Errorf("users_with_quota: want 2 got %v", withQuota)
	}
	if totalUsed != 3350 {
		t.Errorf("used_total_kb: want 3350 got %v", totalUsed)
	}
}

func TestPtfGroupCollector_ComplianceBooleanAndCountByStatus(t *testing.T) {
	c := newPtfGroupCollector()
	res := &bridge.Result{
		Columns: []string{"PTF_GROUP_NAME", "PTF_GROUP_LEVEL", "PTF_GROUP_STATUS", "PTF_GROUP_TARGET_RELEASE"},
		Rows: [][]*string{
			{strPtr("SF99875"), strPtr("42"), strPtr("INSTALLED"), strPtr("V7R5M0")},
			{strPtr("SF99738"), strPtr("10"), strPtr("NOT_INSTALLED"), strPtr("V7R5M0")},
			{strPtr("SF99704"), strPtr("3"), strPtr("NOT_APPLICABLE"), strPtr("V7R5M0")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	installedByGroup := make(map[string]float32, 3)
	for _, dp := range points {
		if dp.Name != "ibmi.ptf_group.installed" {
			continue
		}
		for _, tg := range dp.Tags {
			if tg.Key == "group" {
				installedByGroup[tg.Value] = dp.Value
			}
		}
	}
	if installedByGroup["SF99875"] != 1 {
		t.Errorf("SF99875 should be installed=1, got %v", installedByGroup["SF99875"])
	}
	if installedByGroup["SF99738"] != 0 {
		t.Errorf("SF99738 should be installed=0, got %v", installedByGroup["SF99738"])
	}
	if installedByGroup["SF99704"] != 1 {
		t.Errorf("SF99704 NOT_APPLICABLE should count as installed=1, got %v", installedByGroup["SF99704"])
	}
}

func TestJournalInfoCollector_EmitsRemoteLagWhenPresent(t *testing.T) {
	c := journalInfoCollector{}
	res := &bridge.Result{
		Columns: []string{
			"JOURNAL_NAME", "JOURNAL_LIBRARY", "JOURNAL_STATE", "JOURNAL_TYPE",
			"NUMBER_JOURNAL_RECEIVERS", "TOTAL_SIZE_JOURNAL_RECEIVERS",
			"NUMBER_REMOTE_JOURNALS", "ESTIMATED_TIME_BEHIND", "MAXIMUM_TIME_BEHIND",
		},
		Rows: [][]*string{
			// Journal with remote: lag metrics should be emitted
			{strPtr("QSQJRN"), strPtr("APPLIB"), strPtr("*ACTIVE"), strPtr("*LOCAL"),
				strPtr("3"), strPtr("250000"), strPtr("2"), strPtr("15"), strPtr("42")},
			// Journal without remote: lag columns NULL, no lag metrics
			{strPtr("QAUDJRN"), strPtr("QSYS"), strPtr("*ACTIVE"), strPtr("*LOCAL"),
				strPtr("5"), strPtr("120000"), strPtr("0"), nil, nil},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var lagEstimated, lagMax float32 = -1, -1
	for _, dp := range points {
		if dp.Name == "ibmi.journal.remote_lag_estimated_seconds" {
			lagEstimated = dp.Value
		}
		if dp.Name == "ibmi.journal.remote_lag_maximum_seconds" {
			lagMax = dp.Value
		}
	}
	if lagEstimated != 15 {
		t.Errorf("remote_lag_estimated_seconds: want 15 got %v", lagEstimated)
	}
	if lagMax != 42 {
		t.Errorf("remote_lag_maximum_seconds: want 42 got %v", lagMax)
	}
	// Only one journal had a lag value; the NULL-row must not emit a
	// phantom zero that would pollute dashboards.
	estimatedCount := 0
	for _, dp := range points {
		if dp.Name == "ibmi.journal.remote_lag_estimated_seconds" {
			estimatedCount++
		}
	}
	if estimatedCount != 1 {
		t.Errorf("remote_lag_estimated_seconds should be emitted once, got %d", estimatedCount)
	}
}

func TestDeltaStore_EmitsDeltaAndRateAfterSecondSample(t *testing.T) {
	d := newDeltaStore()
	t0 := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	dp1 := newDatapoint("ibmi.job.cpu_time_ms", 1000, t0, "host=h,job=A")
	if got := d.Derive(dp1); got != nil {
		t.Fatalf("first observation must not emit a rate, got %v", got)
	}
	dp2 := newDatapoint("ibmi.job.cpu_time_ms", 1600, t0.Add(30*time.Second), "host=h,job=A")
	out := d.Derive(dp2)
	if len(out) != 2 {
		t.Fatalf("expected 2 derived points (delta+rate), got %d", len(out))
	}
	var delta, rate float32 = -1, -1
	for _, dp := range out {
		switch dp.Name {
		case "ibmi.job.cpu_time_ms_delta":
			delta = dp.Value
		case "ibmi.job.cpu_time_ms_rate_per_sec":
			rate = dp.Value
		}
	}
	if delta != 600 {
		t.Errorf("delta: want 600 got %v", delta)
	}
	// 600 CPU-ms over 30s → 20 ms/s
	if rate < 19.9 || rate > 20.1 {
		t.Errorf("rate_per_sec: want ~20 got %v", rate)
	}
}

func TestDeltaStore_CounterResetClampsToZero(t *testing.T) {
	d := newDeltaStore()
	t0 := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	_ = d.Derive(newDatapoint("ibmi.job.cpu_time_ms", 5000, t0, "host=h,job=R"))
	// Simulate a job restart: CPU counter resets to a small value.
	out := d.Derive(newDatapoint("ibmi.job.cpu_time_ms", 10, t0.Add(30*time.Second), "host=h,job=R"))
	if len(out) != 2 {
		t.Fatalf("expected 2 points on reset, got %d", len(out))
	}
	for _, dp := range out {
		if dp.Value != 0 {
			t.Errorf("%s: want 0 on counter reset, got %v", dp.Name, dp.Value)
		}
	}
}

// newDatapoint is a local helper that parses the tags argument as a
// simple "k=v,k=v" string into a datapoint.DataPoint. Scoped to tests to
// keep the delta store tests readable.
func newDatapoint(name string, value float32, ts time.Time, tagsSpec string) datapoint.DataPoint {
	var tagList []tags.Tag
	if tagsSpec != "" {
		for _, part := range strings.Split(tagsSpec, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				tagList = append(tagList, tags.Tag{Key: kv[0], Value: kv[1]})
			}
		}
	}
	return datapoint.DataPoint{Name: name, Value: value, Timestamp: ts, Tags: tagList}
}

func TestIndexAdvisorCollector_TotalFromWindowAndRecentBucket(t *testing.T) {
	c := indexAdvisorCollector{}
	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{
			"TABLE_SCHEMA", "TABLE_NAME", "KEY_COLUMNS_ADVISED",
			"TIMES_ADVISED", "MTI_USED", "MTI_CREATED",
			"LAST_ADVISED", "AVERAGE_QUERY_ESTIMATE", "TOTAL_ADVISED",
		},
		Rows: [][]*string{
			// Recent advisory (30 min ago) — should count in recent_1h
			{strPtr("APPLIB"), strPtr("ORDERS"), strPtr("CUSTOMER_ID"),
				strPtr("500"), strPtr("12"), strPtr("3"),
				strPtr("2026-04-17 11:30:00.000000"), strPtr("2.5"), strPtr("42")},
			// Old advisory (2h ago) — not in recent_1h bucket
			{strPtr("APPLIB"), strPtr("ITEMS"), strPtr("SKU"),
				strPtr("300"), strPtr("5"), strPtr("1"),
				strPtr("2026-04-17 10:00:00.000000"), strPtr("1.2"), strPtr("42")},
		},
	}
	points, err := c.Parse(res, "h", ts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var total, recent float32 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.index_advisor.total":
			total = dp.Value
		case "ibmi.index_advisor.recent_advisories_1h":
			recent = dp.Value
		}
	}
	if total != 42 {
		t.Errorf("total (from window fn): want 42 got %v", total)
	}
	if recent != 1 {
		t.Errorf("recent_advisories_1h: want 1 got %v", recent)
	}
}

func TestQuerySupervisorCollector_ElapsedAndUserAggregate(t *testing.T) {
	c := querySupervisorCollector{}
	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{
			"JOB_NAME", "CURRENT_USER_NAME", "CURRENT_TEMPORARY_STORAGE",
			"OPEN_DATE_TIME", "QUERY_TIME_ESTIMATE", "ROWS_FETCHED",
			"QUERY_TYPE", "TOTAL_ACTIVE",
		},
		Rows: [][]*string{
			{strPtr("001/QSYS/QZDASOINIT"), strPtr("ALICE"), strPtr("25"),
				strPtr("2026-04-17 11:59:30.000000"), strPtr("500"), strPtr("10000"),
				strPtr("QUERY"), strPtr("3")},
			{strPtr("002/QSYS/QZDASOINIT"), strPtr("ALICE"), strPtr("15"),
				strPtr("2026-04-17 11:58:00.000000"), strPtr("200"), strPtr("500"),
				strPtr("QUERY"), strPtr("3")},
			{strPtr("003/QSYS/QZDASOINIT"), strPtr("BOB"), strPtr("5"),
				strPtr("2026-04-17 11:55:00.000000"), strPtr("50"), strPtr("100"),
				strPtr("REFRESH"), strPtr("3")},
		},
	}
	points, err := c.Parse(res, "h", ts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	aliceCount, bobCount := float32(-1), float32(-1)
	var total float32 = -1
	elapsedFirst := float32(-1)
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.query_supervisor.active_total":
			total = dp.Value
		case "ibmi.query_supervisor.active_by_user":
			for _, tg := range dp.Tags {
				if tg.Key == "user" && tg.Value == "ALICE" {
					aliceCount = dp.Value
				}
				if tg.Key == "user" && tg.Value == "BOB" {
					bobCount = dp.Value
				}
			}
		case "ibmi.query_supervisor.elapsed_seconds":
			for _, tg := range dp.Tags {
				if tg.Key == "job_name" && tg.Value == "001/QSYS/QZDASOINIT" {
					elapsedFirst = dp.Value
				}
			}
		}
	}
	if total != 3 {
		t.Errorf("active_total: want 3 got %v", total)
	}
	if aliceCount != 2 {
		t.Errorf("active_by_user ALICE: want 2 got %v", aliceCount)
	}
	if bobCount != 1 {
		t.Errorf("active_by_user BOB: want 1 got %v", bobCount)
	}
	// 001 was opened 30s before ts
	if elapsedFirst < 29 || elapsedFirst > 31 {
		t.Errorf("elapsed_seconds for 001: want ~30, got %v", elapsedFirst)
	}
}

func TestNetstatConnectionCollector_AggregatesByState(t *testing.T) {
	c := netstatConnectionCollector{}
	res := &bridge.Result{
		Columns: []string{"TCP_STATE", "CONN_COUNT"},
		Rows: [][]*string{
			{strPtr("ESTABLISHED"), strPtr("42")},
			{strPtr("TIME_WAIT"), strPtr("120")},
			{strPtr("CLOSE_WAIT"), strPtr("8")},
			{strPtr("LISTEN"), strPtr("18")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var total float32 = -1
	byState := make(map[string]float32, 4)
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.netstat.connections_total":
			total = dp.Value
		case "ibmi.netstat.connections_by_state":
			for _, tg := range dp.Tags {
				if tg.Key == "tcp_state" {
					byState[tg.Value] = dp.Value
				}
			}
		}
	}
	if total != 188 {
		t.Errorf("connections_total: want 188 got %v", total)
	}
	if byState["ESTABLISHED"] != 42 {
		t.Errorf("ESTABLISHED: want 42 got %v", byState["ESTABLISHED"])
	}
	if byState["CLOSE_WAIT"] != 8 {
		t.Errorf("CLOSE_WAIT: want 8 got %v", byState["CLOSE_WAIT"])
	}
}

func TestAuthorityCollectionCollector_EmptyEmitsZeroTotals(t *testing.T) {
	c := newAuthorityCollectionCollector()
	res := &bridge.Result{
		Columns: []string{"USER_NAME", "ENTRY_COUNT", "FAILED_CHECKS"},
		Rows:    nil,
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var entries, failed float32 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.authority_collection.entries_total":
			entries = dp.Value
		case "ibmi.authority_collection.failed_checks_total":
			failed = dp.Value
		}
	}
	if entries != 0 {
		t.Errorf("entries_total (empty): want 0 got %v", entries)
	}
	if failed != 0 {
		t.Errorf("failed_checks_total (empty): want 0 got %v", failed)
	}
}

func TestAuthorityCollectionCollector_AggregatesByUser(t *testing.T) {
	c := newAuthorityCollectionCollector()
	res := &bridge.Result{
		Columns: []string{"USER_NAME", "ENTRY_COUNT", "FAILED_CHECKS"},
		Rows: [][]*string{
			{strPtr("ALICE"), strPtr("100"), strPtr("2")},
			{strPtr("BOB"), strPtr("50"), strPtr("0")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var totalEntries, totalFailed float32 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.authority_collection.entries_total":
			totalEntries = dp.Value
		case "ibmi.authority_collection.failed_checks_total":
			totalFailed = dp.Value
		}
	}
	if totalEntries != 150 {
		t.Errorf("entries_total: want 150 got %v", totalEntries)
	}
	if totalFailed != 2 {
		t.Errorf("failed_checks_total: want 2 got %v", totalFailed)
	}
}

func TestMessageQueueCollector_NameSchemeAndLegacyCompat(t *testing.T) {
	// QSYSOPR keeps the legacy identifier for backward compatibility
	// with pre-existing dashboards.
	legacy := newMessageQueueCollectorFor("QSYS", "QSYSOPR", 0)
	if legacy.Name() != "message_queue" {
		t.Errorf("QSYSOPR should report legacy name, got %q", legacy.Name())
	}
	// Any other queue gets a suffixed name.
	qsysmsg := newMessageQueueCollectorFor("QSYS", "QSYSMSG", 30)
	if qsysmsg.Name() != "message_queue_qsysmsg" {
		t.Errorf("QSYSMSG should report suffixed name, got %q", qsysmsg.Name())
	}
	if qsysmsg.minSeverity != 30 {
		t.Errorf("QSYSMSG min_severity: want 30 got %d", qsysmsg.minSeverity)
	}
	// Library + name uppercased to match IBM i naming conventions.
	app := newMessageQueueCollectorFor("applib ", " myqueue", 50)
	if app.queueLib != "APPLIB" || app.queueName != "MYQUEUE" {
		t.Errorf("expected uppercased trimmed lib/name, got %q / %q", app.queueLib, app.queueName)
	}
}

func TestExpandMessageQueues_NoConfigKeepsBase(t *testing.T) {
	base := []collector{
		systemStatusCollector{},
		newMessageQueueCollector(),
	}
	out := expandMessageQueues(probeConfig{}, base)
	if len(out) != 2 {
		t.Fatalf("no-config expansion should keep base, got len %d", len(out))
	}
}

func TestExpandMessageQueues_ReplacesDefaultWithConfigList(t *testing.T) {
	base := []collector{
		systemStatusCollector{},
		newMessageQueueCollector(),
		jobQueueCollector{},
	}
	cfg := probeConfig{
		MessageQueues: []messageQueueSpec{
			{Library: "QSYS", Name: "QSYSOPR", MinSeverity: 0},
			{Library: "QSYS", Name: "QSYSMSG", MinSeverity: 30},
		},
	}
	out := expandMessageQueues(cfg, base)
	names := make([]string, len(out))
	for i, c := range out {
		names[i] = c.Name()
	}
	// The default "message_queue" is consumed and replaced by the two
	// configured queues (QSYSOPR keeps the legacy name, QSYSMSG gets
	// the suffixed one).
	foundLegacy, foundQsysmsg := false, false
	for _, n := range names {
		if n == "message_queue" {
			foundLegacy = true
		}
		if n == "message_queue_qsysmsg" {
			foundQsysmsg = true
		}
	}
	if !foundLegacy || !foundQsysmsg {
		t.Errorf("expected both message_queue and message_queue_qsysmsg, got %v", names)
	}
	// The surrounding collectors (system_status, job_queue) must be
	// preserved — we never want expansion to collaterally drop them.
	foundSystem, foundJobQueue := false, false
	for _, n := range names {
		if n == "system_status" {
			foundSystem = true
		}
		if n == "job_queue" {
			foundJobQueue = true
		}
	}
	if !foundSystem || !foundJobQueue {
		t.Errorf("expansion dropped surrounding collectors: %v", names)
	}
}

func TestExpandMessageQueues_NoOpWhenDefaultAbsent(t *testing.T) {
	// If the operator disabled message_queue (e.g. via
	// disabled_collectors), the expansion must respect that and not
	// re-inject queues from the config.
	base := []collector{systemStatusCollector{}, jobQueueCollector{}}
	cfg := probeConfig{
		MessageQueues: []messageQueueSpec{
			{Library: "QSYS", Name: "QSYSMSG", MinSeverity: 30},
		},
	}
	out := expandMessageQueues(cfg, base)
	if len(out) != 2 {
		t.Fatalf("expansion must be no-op when default is absent, got len %d", len(out))
	}
	for _, c := range out {
		if c.Name() == "message_queue_qsysmsg" {
			t.Errorf("unexpected expansion: got %q even though default was disabled", c.Name())
		}
	}
}

func TestHardwareResourceCollector_AggregatesAndNonOperationalTotal(t *testing.T) {
	c := hardwareResourceCollector{}
	res := &bridge.Result{
		Columns: []string{"RESOURCE_CATEGORY", "STATUS", "RESOURCE_COUNT"},
		Rows: [][]*string{
			{strPtr("COMM"), strPtr("OPERATIONAL"), strPtr("14")},
			{strPtr("STORAGE"), strPtr("OPERATIONAL"), strPtr("21")},
			{strPtr("STORAGE"), strPtr("NOT OPERATIONAL"), strPtr("2")},
			{strPtr("PROCESSOR"), strPtr("OPERATIONAL"), strPtr("4")},
			{strPtr("WORKSTATION"), strPtr("FAILED"), strPtr("1")},
		},
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var total, nonOp float32 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.hardware_resource.total":
			total = dp.Value
		case "ibmi.hardware_resource.non_operational_total":
			nonOp = dp.Value
		}
	}
	if total != 42 {
		t.Errorf("total: want 42 got %v", total)
	}
	// 2 STORAGE NOT OPERATIONAL + 1 WORKSTATION FAILED = 3
	if nonOp != 3 {
		t.Errorf("non_operational_total: want 3 got %v", nonOp)
	}
}

func TestServiceAgentCollector_ActivationAndAge(t *testing.T) {
	c := newServiceAgentCollector()
	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{
			"ACTIVATION_STATUS", "LAST_INVENTORY_COLLECTION_TIMESTAMP",
			"LAST_HARDWARE_PROBLEM_SEND", "LAST_SOFTWARE_PROBLEM_SEND",
		},
		Rows: [][]*string{
			{strPtr("ENABLED"), strPtr("2026-04-17 11:00:00.000000"), nil, nil},
		},
	}
	points, err := c.Parse(res, "h", ts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var activated, invAge float32 = -1, -1
	for _, dp := range points {
		switch dp.Name {
		case "ibmi.service_agent.activated":
			activated = dp.Value
		case "ibmi.service_agent.last_inventory_age_seconds":
			invAge = dp.Value
		}
	}
	if activated != 1 {
		t.Errorf("activated (ENABLED): want 1 got %v", activated)
	}
	if invAge < 3599 || invAge > 3601 {
		t.Errorf("last_inventory_age_seconds: want ~3600 got %v", invAge)
	}
	// No datapoint should be emitted for the NULL timestamps (guards
	// against phantom zeros on deployments that have never sent a
	// problem report).
	for _, dp := range points {
		if dp.Name == "ibmi.service_agent.last_hw_problem_send_age_seconds" {
			t.Errorf("hw_problem age emitted despite NULL: %v", dp.Value)
		}
	}
}

func TestServiceAgentCollector_NotActivatedWhenEmpty(t *testing.T) {
	c := newServiceAgentCollector()
	res := &bridge.Result{
		Columns: []string{"ACTIVATION_STATUS"},
		Rows:    nil,
	}
	points, err := c.Parse(res, "h", testTs)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(points) != 1 || points[0].Value != 0 {
		t.Errorf("empty ESA view should emit activated=0, got %v", points)
	}
}

func TestWatchInfoCollector_PerSessionAgeAndTotal(t *testing.T) {
	c := newWatchInfoCollector()
	ts := time.Date(2026, 4, 17, 12, 0, 0, 0, time.Local)
	res := &bridge.Result{
		Columns: []string{
			"SESSION_ID", "WATCH_PROGRAM_NAME", "WATCH_PROGRAM_LIBRARY",
			"ORIGIN_JOB", "SESSION_START_TIMESTAMP",
		},
		Rows: [][]*string{
			{strPtr("W001"), strPtr("MYWATCH"), strPtr("QGPL"),
				strPtr("1/QSECOFR/QPADEV"), strPtr("2026-04-17 11:30:00.000000")},
			{strPtr("W002"), strPtr("CRITICAL"), strPtr("QSYS"),
				strPtr("2/QSYS/QSYSCOMM"), strPtr("2026-04-17 10:00:00.000000")},
		},
	}
	points, err := c.Parse(res, "h", ts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var total float32 = -1
	ageBySession := make(map[string]float32, 2)
	for _, dp := range points {
		if dp.Name == "ibmi.watch.active_total" {
			total = dp.Value
		}
		if dp.Name == "ibmi.watch.session_age_seconds" {
			for _, tg := range dp.Tags {
				if tg.Key == "session_id" {
					ageBySession[tg.Value] = dp.Value
				}
			}
		}
	}
	if total != 2 {
		t.Errorf("active_total: want 2 got %v", total)
	}
	if ageBySession["W001"] < 1799 || ageBySession["W001"] > 1801 {
		t.Errorf("W001 age: want ~1800, got %v", ageBySession["W001"])
	}
	if ageBySession["W002"] < 7199 || ageBySession["W002"] > 7201 {
		t.Errorf("W002 age: want ~7200, got %v", ageBySession["W002"])
	}
}

func TestParseMessageQueues_ConfigMapping(t *testing.T) {
	raw := map[string]interface{}{
		"host":              "h",
		"user":              "u",
		"password":          "p",
		"bridge_runner_dir": "/tmp",
		"message_queues": []interface{}{
			map[string]interface{}{"name": "QSYSOPR"},
			map[string]interface{}{"library": "QGPL", "name": "APPQ", "min_severity": 40},
		},
	}
	cfg, err := parseProbeConfig(raw)
	if err != nil {
		t.Fatalf("parseProbeConfig: %v", err)
	}
	if len(cfg.MessageQueues) != 2 {
		t.Fatalf("want 2 queues, got %d", len(cfg.MessageQueues))
	}
	if cfg.MessageQueues[0].Library != "QSYS" {
		t.Errorf("default library: want QSYS got %q", cfg.MessageQueues[0].Library)
	}
	if cfg.MessageQueues[1].MinSeverity != 40 {
		t.Errorf("min_severity: want 40 got %d", cfg.MessageQueues[1].MinSeverity)
	}
}
