package ibmi

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/ibmi/bridge"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// fakeExecutor is a hand-rolled stub satisfying queryExecutor. We avoid
// pulling in a mocking library — the surface is two methods and we need
// per-query scripted responses to exercise the collector-iteration
// paths.
type fakeExecutor struct {
	// responses maps SQL text → scripted result (one of result/err is set).
	responses map[string]fakeResponse
	calls     []string
	closed    bool
}

type fakeResponse struct {
	result *bridge.Result
	err    error
}

func (f *fakeExecutor) Query(_ context.Context, sql string) (*bridge.Result, error) {
	f.calls = append(f.calls, sql)
	resp, ok := f.responses[sql]
	if !ok {
		return nil, errors.New("no scripted response for SQL: " + sql)
	}
	return resp.result, resp.err
}

func (f *fakeExecutor) Close(_ context.Context) error { f.closed = true; return nil }

func strPtr(s string) *string { return &s }

func testConfig() probeConfig {
	return probeConfig{
		Host:         "testhost",
		User:         "U",
		Password:     "P",
		Interval:     30 * time.Second,
		QueryTimeout: time.Second,
	}
}

func testLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "probe.ibmi")
}

// newTestProbe constructs an ibmiProbe wired to a fake executor and the
// provided collector list. Unit tests inject whichever collectors they
// need to exercise a specific path.
func newTestProbe(t *testing.T, exec queryExecutor, collectors []collector) *ibmiProbe {
	t.Helper()
	return newIbmiProbeWithExecutor(testConfig(), testLogger(), exec, collectors)
}

// systemStatusRow builds a synthetic SYSTEM_STATUS_INFO result matching
// the column order emitted by systemStatusCollector.SQL().
func systemStatusRow() *bridge.Result {
	return &bridge.Result{
		Columns: []string{
			"ELAPSED_CPU_USED", "CONFIGURED_CPUS", "CURRENT_CPU_CAPACITY",
			"MAIN_STORAGE_SIZE", "SYSTEM_ASP_USED", "TOTAL_JOBS_IN_SYSTEM",
		},
		Rows: [][]*string{{
			strPtr("12.50"), strPtr("4"), strPtr("2.00"),
			strPtr("100663296"), strPtr("65.71"), strPtr("1655"),
		}},
	}
}

// TestCollect_SystemStatusHappyPath asserts that a full SYSTEM_STATUS_INFO
// row produces the six expected DataPoints plus the four health
// DataPoints (success_total, failure_total, last_duration_ms,
// last_success_timestamp) for the single collector.
func TestCollect_SystemStatusHappyPath(t *testing.T) {
	c := systemStatusCollector{}
	exec := &fakeExecutor{
		responses: map[string]fakeResponse{
			c.SQL(): {result: systemStatusRow()},
		},
	}
	p := newTestProbe(t, exec, []collector{c})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// 6 metrics + 4 health = 10 datapoints.
	if len(points) != 10 {
		t.Fatalf("expected 10 datapoints, got %d", len(points))
	}

	byName := make(map[string]datapoint.DataPoint, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp
	}

	wantValues := map[string]float32{
		"ibmi.cpu.elapsed_used_percent": 12.50,
		"ibmi.cpu.configured_count":     4,
		"ibmi.cpu.current_capacity":     2.00,
		"ibmi.memory.main_storage_kb":   100663296,
		"ibmi.asp.system_used_percent":  65.71,
		"ibmi.jobs.total_count":         1655,
	}
	for name, v := range wantValues {
		dp, ok := byName[name]
		if !ok {
			t.Errorf("missing metric %s", name)
			continue
		}
		if dp.Value != v {
			t.Errorf("%s: want %v got %v", name, v, dp.Value)
		}
		if !hasTag(dp, "host", "testhost") || !hasTag(dp, "probe_name", probeType) || !hasTag(dp, "probe_type", probeType) {
			t.Errorf("%s: missing expected tags %#v", name, dp.Tags)
		}
	}

	// Health metrics for the single collector.
	for _, name := range []string{
		"ibmi.collector.success_total",
		"ibmi.collector.failure_total",
		"ibmi.collector.last_duration_ms",
		"ibmi.collector.last_success_timestamp",
	} {
		dp, ok := byName[name]
		if !ok {
			t.Errorf("missing health metric %s", name)
			continue
		}
		if !hasTag(dp, "collector", "system_status") {
			t.Errorf("%s: missing collector tag", name)
		}
	}
	if byName["ibmi.collector.success_total"].Value != 1 {
		t.Errorf("success_total: want 1 got %v", byName["ibmi.collector.success_total"].Value)
	}
	if byName["ibmi.collector.failure_total"].Value != 0 {
		t.Errorf("failure_total: want 0 got %v", byName["ibmi.collector.failure_total"].Value)
	}
}

// TestCollect_PartialFailureDoesNotBlockSiblings is the critical
// invariant of the collector loop: one collector failing must not
// prevent the others from running.
func TestCollect_PartialFailureDoesNotBlockSiblings(t *testing.T) {
	good := systemStatusCollector{}
	bad := fakeFailingCollector{name: "always_fails", sql: "SELECT 1 FROM BROKEN"}

	exec := &fakeExecutor{
		responses: map[string]fakeResponse{
			good.SQL(): {result: systemStatusRow()},
			bad.SQL():  {err: errors.New("SQL0204 TABLE NOT FOUND")},
		},
	}
	p := newTestProbe(t, exec, []collector{bad, good})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := make(map[string]datapoint.DataPoint, len(points))
	for _, dp := range points {
		byName[dp.Name] = dp
	}

	// system_status still produced its six metrics.
	if _, ok := byName["ibmi.cpu.elapsed_used_percent"]; !ok {
		t.Error("healthy sibling collector did not emit its metrics")
	}

	// The failing collector incremented failure_total.
	var failureForBad float32
	var successForBad float32
	for _, dp := range points {
		if dp.Name == "ibmi.collector.failure_total" && hasTag(dp, "collector", "always_fails") {
			failureForBad = dp.Value
		}
		if dp.Name == "ibmi.collector.success_total" && hasTag(dp, "collector", "always_fails") {
			successForBad = dp.Value
		}
	}
	if failureForBad != 1 {
		t.Errorf("bad.failure_total: want 1 got %v", failureForBad)
	}
	if successForBad != 0 {
		t.Errorf("bad.success_total: want 0 got %v", successForBad)
	}

	// Both SQLs were actually dispatched.
	if len(exec.calls) != 2 {
		t.Errorf("expected 2 query calls, got %d", len(exec.calls))
	}
}

// TestCollect_HealthCountersAccumulate verifies that calling Collect
// twice adds to the counters rather than replacing them — health
// metrics are running totals, not per-cycle snapshots.
func TestCollect_HealthCountersAccumulate(t *testing.T) {
	c := systemStatusCollector{}
	exec := &fakeExecutor{
		responses: map[string]fakeResponse{c.SQL(): {result: systemStatusRow()}},
	}
	p := newTestProbe(t, exec, []collector{c})

	if _, err := p.Collect(); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("cycle 2: %v", err)
	}

	var success float32
	for _, dp := range points {
		if dp.Name == "ibmi.collector.success_total" && hasTag(dp, "collector", "system_status") {
			success = dp.Value
		}
	}
	if success != 2 {
		t.Errorf("success_total after 2 cycles: want 2 got %v", success)
	}
}

func TestCollect_NullColumnIsSkipped(t *testing.T) {
	c := systemStatusCollector{}
	res := systemStatusRow()
	res.Rows[0][0] = nil // ELAPSED_CPU_USED becomes NULL
	exec := &fakeExecutor{
		responses: map[string]fakeResponse{c.SQL(): {result: res}},
	}
	p := newTestProbe(t, exec, []collector{c})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "ibmi.cpu.elapsed_used_percent" {
			t.Error("null column should have been skipped, but metric was emitted")
		}
	}
}

func TestOnShutdown_ClosesExecutor(t *testing.T) {
	exec := &fakeExecutor{responses: map[string]fakeResponse{}}
	p := newTestProbe(t, exec, nil)
	if err := p.OnShutdown(context.Background()); err != nil {
		t.Fatalf("OnShutdown: %v", err)
	}
	if !exec.closed {
		t.Fatal("executor was not closed")
	}
}

// hasTag is a tiny assertion helper used across the tests above.
func hasTag(dp datapoint.DataPoint, key, value string) bool {
	for _, t := range dp.Tags {
		if t.Key == key && t.Value == value {
			return true
		}
	}
	return false
}

// fakeFailingCollector is a minimal collector stub that returns a
// scripted SQL string the fakeExecutor is configured to error on.
type fakeFailingCollector struct {
	name string
	sql  string
}

func (f fakeFailingCollector) Name() string  { return f.name }
func (f fakeFailingCollector) SQL() string   { return f.sql }
func (f fakeFailingCollector) IsEvent() bool { return false }
func (f fakeFailingCollector) Parse(_ *bridge.Result, _ string, _ time.Time) ([]datapoint.DataPoint, error) {
	return nil, errors.New("should not be reached: fakeFailingCollector always fails at Query")
}

func TestParseProbeConfig(t *testing.T) {
	cases := []struct {
		name      string
		raw       map[string]interface{}
		wantErr   bool
		wantHost  string
		wantEvery time.Duration
	}{
		{
			name: "minimal valid",
			raw: map[string]interface{}{
				"host":              "h",
				"user":              "u",
				"password":          "p",
				"bridge_runner_dir": "/tmp/r",
			},
			wantHost:  "h",
			wantEvery: 30 * time.Second,
		},
		{
			name: "interval as int",
			raw: map[string]interface{}{
				"host": "h", "user": "u", "password": "p",
				"bridge_runner_dir": "/tmp/r",
				"interval":          60,
			},
			wantEvery: 60 * time.Second,
		},
		{
			name: "interval as float64 (json origin)",
			raw: map[string]interface{}{
				"host": "h", "user": "u", "password": "p",
				"bridge_runner_dir": "/tmp/r",
				"interval":          float64(45),
			},
			wantEvery: 45 * time.Second,
		},
		{
			name: "missing host",
			raw: map[string]interface{}{
				"user": "u", "password": "p", "bridge_runner_dir": "/tmp/r",
			},
			wantErr: true,
		},
		{
			name: "wrong type",
			raw: map[string]interface{}{
				"host": 42, "user": "u", "password": "p", "bridge_runner_dir": "/tmp/r",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseProbeConfig(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantHost != "" && cfg.Host != tc.wantHost {
				t.Errorf("host: got %q want %q", cfg.Host, tc.wantHost)
			}
			if tc.wantEvery != 0 && cfg.Interval != tc.wantEvery {
				t.Errorf("interval: got %v want %v", cfg.Interval, tc.wantEvery)
			}
		})
	}
}
