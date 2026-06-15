package execprobe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// writeScript materializes a check script for the host OS and returns
// the command and args to run it.
func writeScript(t *testing.T, unixBody, batBody string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "check.bat")
		if err := os.WriteFile(path, []byte(batBody), 0o700); err != nil {
			t.Fatalf("writing script: %v", err)
		}
		return path
	}
	path := filepath.Join(dir, "check.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+unixBody), 0o700); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	return path
}

func newTestProbe(t *testing.T, config map[string]interface{}) *ExecProbe {
	t.Helper()
	probe, err := NewExecProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewExecProbe: %v", err)
	}
	p, ok := probe.(*ExecProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("custom-check")
	return p
}

func collectByName(t *testing.T, p *ExecProbe) map[string]float32 {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float32{}
	for _, dp := range points {
		got[dp.Name] = dp.Value
	}
	return got
}

func TestParseConfig_Errors(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"missing command": {},
		"relative path":   {"command": "check_disk"},
		"bad format":      {"command": "/bin/true", "format": "xml"},
		"bad args":        {"command": "/bin/true", "args": "not-a-list"},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewExecProbe(config, testBaseLogger()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

func TestCheckExecutable_RefusesWorldWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission model only")
	}
	path := filepath.Join(t.TempDir(), "check.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("writing script: %v", err)
	}
	// WriteFile's mode goes through the umask, which strips the
	// world-writable bit; set it explicitly.
	if err := os.Chmod(path, 0o707); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, err := NewExecProbe(map[string]interface{}{"command": path}, testBaseLogger()); err == nil {
		t.Fatal("expected world-writable executable to be refused")
	}
}

func TestParseNagiosPerfdata(t *testing.T) {
	out := []byte("DISK OK - free space ok | /=2643MB;5948;5958;0;5968 'load avg'=0.5 time=23ms count=12c pct=88%\n")
	metrics := parseNagiosPerfdata(out)
	got := map[string]checkMetric{}
	for _, m := range metrics {
		got[m.name] = m
	}

	if len(metrics) != 5 {
		t.Fatalf("parsed %d metrics, want 5: %+v", len(metrics), metrics)
	}
	if m := got["senhub.exec.load_avg"]; m.value != 0.5 || m.otelType != "gauge" {
		t.Errorf("quoted label: %+v", m)
	}
	if m := got["senhub.exec.time"]; m.value != 0.023 {
		t.Errorf("ms not normalized to seconds: %+v", m)
	}
	if m := got["senhub.exec.count"]; m.otelType != "counter" || m.value != 12 {
		t.Errorf("c UOM must mark a counter: %+v", m)
	}
	if m := got["senhub.exec.pct"]; m.value != 88 {
		t.Errorf("percent value must stay as-is: %+v", m)
	}
	if m := got["senhub.exec.perfdata"]; m.value != 2643*1024*1024 {
		t.Errorf("\"/\" label must land on the fallback name with MB scaled to bytes: %+v", m)
	}
}

func TestParseNagiosPerfdata_EdgeCases(t *testing.T) {
	if got := parseNagiosPerfdata([]byte("OK - no perfdata here")); got != nil {
		t.Errorf("no pipe must yield nil, got %+v", got)
	}
	got := parseNagiosPerfdata([]byte("OK | good=1 broken=abc =2 also_good=3"))
	if len(got) != 2 {
		t.Errorf("malformed tokens must be skipped, got %+v", got)
	}
}

func TestParseJSONOutput(t *testing.T) {
	stdout := []byte(`{"status": 1, "metrics": [
		{"name": "queue.depth", "value": 12, "tags": {"queue": "orders"}},
		{"name": "Processed Total", "value": 400, "type": "counter"}
	]}`)
	status, metrics, err := parseJSONOutput(stdout, 0)
	if err != nil {
		t.Fatalf("parseJSONOutput: %v", err)
	}
	if status != 1 {
		t.Errorf("status = %d, want 1 (explicit overrides exit code)", status)
	}
	if len(metrics) != 2 {
		t.Fatalf("metrics = %+v, want 2", metrics)
	}
	if metrics[0].name != "senhub.exec.queue.depth" || metrics[0].tags["queue"] != "orders" {
		t.Errorf("metric 0: %+v", metrics[0])
	}
	if metrics[1].name != "senhub.exec.processed_total" || metrics[1].otelType != "counter" {
		t.Errorf("metric 1: %+v", metrics[1])
	}

	if status, _, _ := parseJSONOutput([]byte(`{"metrics": []}`), 2); status != 2 {
		t.Errorf("missing status must fall back to exit code, got %d", status)
	}
	if _, _, err := parseJSONOutput([]byte(`{"status": 9}`), 0); err == nil {
		t.Error("out-of-range status must error")
	}
	if _, _, err := parseJSONOutput([]byte(`not json`), 0); err == nil {
		t.Error("bad JSON must error")
	}
}

// TestCollect_NagiosEndToEnd runs a REAL script through the production
// path: status from the exit code, perfdata parsed into metrics.
func TestCollect_NagiosEndToEnd(t *testing.T) {
	script := writeScript(t,
		"echo 'WARN - queue high | depth=42;10;50'\nexit 1\n",
		"@echo WARN - queue high ^| depth=42;10;50\r\n@exit /b 1\r\n",
	)
	p := newTestProbe(t, map[string]interface{}{"command": script})
	got := collectByName(t, p)

	if got["senhub.exec.status"] != 1 {
		t.Errorf("status = %v, want 1 (warning)", got["senhub.exec.status"])
	}
	if got["senhub.exec.depth"] != 42 {
		t.Errorf("depth = %v, want 42", got["senhub.exec.depth"])
	}
	if got["senhub.exec.timeout"] != 0 {
		t.Errorf("timeout = %v, want 0", got["senhub.exec.timeout"])
	}
	if _, ok := got["senhub.exec.duration"]; !ok {
		t.Error("missing duration")
	}
}

func TestCollect_JSONEndToEnd(t *testing.T) {
	script := writeScript(t,
		`echo '{"status": 0, "metrics": [{"name": "sessions", "value": 7}]}'`+"\n",
		`@echo {"status": 0, "metrics": [{"name": "sessions", "value": 7}]}`+"\r\n",
	)
	p := newTestProbe(t, map[string]interface{}{"command": script, "format": "json"})
	got := collectByName(t, p)

	if got["senhub.exec.status"] != 0 {
		t.Errorf("status = %v, want 0", got["senhub.exec.status"])
	}
	if got["senhub.exec.sessions"] != 7 {
		t.Errorf("sessions = %v, want 7", got["senhub.exec.sessions"])
	}
}

// TestCollect_TimeoutKills pins the hard-timeout semantics: the run is
// killed, reported as timeout, and the status is unknown.
func TestCollect_TimeoutKills(t *testing.T) {
	script := writeScript(t,
		"sleep 30\n",
		"@ping -n 31 127.0.0.1 >nul\r\n",
	)
	p := newTestProbe(t, map[string]interface{}{"command": script, "timeout": 1})

	start := time.Now()
	got := collectByName(t, p)
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Fatalf("timeout did not kill the run (took %v)", elapsed)
	}
	if got["senhub.exec.timeout"] != 1 {
		t.Errorf("timeout = %v, want 1", got["senhub.exec.timeout"])
	}
	if got["senhub.exec.status"] != 3 {
		t.Errorf("status = %v, want 3 (unknown) on timeout", got["senhub.exec.status"])
	}
}

// TestCollect_ConcurrentRunSkipped pins the guard: while a run is in
// flight, the next cycle reports skipped instead of stacking processes.
func TestCollect_ConcurrentRunSkipped(t *testing.T) {
	script := writeScript(t, "exit 0\n", "@exit /b 0\r\n")
	p := newTestProbe(t, map[string]interface{}{"command": script})

	block := make(chan struct{})
	started := make(chan struct{})
	p.run = func() execResult {
		close(started)
		<-block
		return execResult{}
	}

	done := make(chan []data_store.DataPoint)
	go func() {
		points, _ := p.Collect()
		done <- points
	}()
	<-started

	got := collectByName(t, p)
	if got["senhub.exec.skipped"] != 1 {
		t.Errorf("overlapping Collect must report skipped=1, got %v", got)
	}

	close(block)
	<-done
}

func TestCollect_SeamStatusMapping(t *testing.T) {
	script := writeScript(t, "exit 0\n", "@exit /b 0\r\n")
	p := newTestProbe(t, map[string]interface{}{"command": script})

	cases := map[int]float32{0: 0, 1: 1, 2: 2, 5: 3}
	for exit, want := range cases {
		p.run = func() execResult { return execResult{exitCode: exit} }
		got := collectByName(t, p)
		if got["senhub.exec.status"] != want {
			t.Errorf("exit %d: status = %v, want %v", exit, got["senhub.exec.status"], want)
		}
	}

	p.run = func() execResult { return execResult{err: os.ErrPermission} }
	if got := collectByName(t, p); got["senhub.exec.status"] != 3 {
		t.Errorf("spawn failure: status = %v, want 3", got["senhub.exec.status"])
	}
}
