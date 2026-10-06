package otlp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "otlp-state-")
	if err != nil {
		os.Exit(1)
	}
	_ = os.Setenv("STATE_DIRECTORY", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func clearQueueEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"SENHUB_LOG_QUEUE", "SENHUB_LOG_QUEUE_RETENTION", "SENHUB_LOG_QUEUE_MAX_BYTES"} {
		t.Setenv(k, "")
	}
}

func parseWith(t *testing.T, extra map[string]interface{}) Config {
	t.Helper()
	params := map[string]interface{}{"endpoint": "127.0.0.1:65000"}
	for k, v := range extra {
		params[k] = v
	}
	cfg, err := ParseConfig(params)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

func TestLogsQueueDefault_OnUnderStateDir(t *testing.T) {
	clearQueueEnv(t)
	state := t.TempDir()
	t.Setenv("STATE_DIRECTORY", state)
	cfg := parseWith(t, nil)
	if !cfg.Persistence.Enabled || cfg.Persistence.LogsQueueMaxAge != 24*time.Hour {
		t.Fatalf("defaults: %+v", cfg.Persistence)
	}
	if got, want := cfg.Persistence.logsQueuePath(), filepath.Join(state, "otlp-queue"); got != want {
		t.Errorf("queue path = %q, want %q", got, want)
	}
	if cfg.Persistence.Path != "" {
		t.Errorf("checkpoint path must stay opt-in, got %q", cfg.Persistence.Path)
	}
}

func TestLogsQueueDefault_ContainerStateDirEnv(t *testing.T) {
	clearQueueEnv(t)
	t.Setenv("STATE_DIRECTORY", "")
	state := t.TempDir()
	t.Setenv("SENHUB_STATE_DIR", state)
	cfg := parseWith(t, nil)
	if got, want := cfg.Persistence.logsQueuePath(), filepath.Join(state, "otlp-queue"); got != want {
		t.Errorf("queue path = %q, want %q", got, want)
	}
}

func TestLogsQueueDefault_DisabledByConfig(t *testing.T) {
	clearQueueEnv(t)
	cfg := parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{"enabled": false}})
	if cfg.Persistence.logsQueuePath() != "" {
		t.Errorf("queue still on: %q", cfg.Persistence.logsQueuePath())
	}
}

func TestLogsQueueDefault_EnvWinsOverFile(t *testing.T) {
	clearQueueEnv(t)
	t.Setenv("SENHUB_LOG_QUEUE", "false")
	cfg := parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{"enabled": true}})
	if cfg.Persistence.logsQueuePath() != "" {
		t.Errorf("env false must disable, got %q", cfg.Persistence.logsQueuePath())
	}

	t.Setenv("SENHUB_LOG_QUEUE", "true")
	t.Setenv("SENHUB_LOG_QUEUE_RETENTION", "2h")
	t.Setenv("SENHUB_LOG_QUEUE_MAX_BYTES", "64MiB")
	cfg = parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{
		"enabled": false, "logs_queue_max_age": "48h", "logs_queue_max_bytes": 1,
	}})
	if !cfg.Persistence.Enabled || cfg.Persistence.LogsQueueMaxAge != 2*time.Hour || cfg.Persistence.LogsQueueMaxBytes != 64<<20 {
		t.Errorf("env did not win: %+v", cfg.Persistence)
	}
}

func TestLogsQueueDefault_InvalidEnvRejected(t *testing.T) {
	for k, v := range map[string]string{
		"SENHUB_LOG_QUEUE": "maybe", "SENHUB_LOG_QUEUE_RETENTION": "soon", "SENHUB_LOG_QUEUE_MAX_BYTES": "big",
	} {
		t.Run(k, func(t *testing.T) {
			clearQueueEnv(t)
			t.Setenv(k, v)
			if _, err := ParseConfig(map[string]interface{}{"endpoint": "x:1"}); err == nil || !strings.Contains(err.Error(), k) {
				t.Errorf("want error naming %s, got %v", k, err)
			}
		})
	}
}

func TestLogsQueueDefault_ExplicitPathUnchanged(t *testing.T) {
	clearQueueEnv(t)
	dir := t.TempDir()
	cfg := parseWith(t, map[string]interface{}{"persistence": map[string]interface{}{"path": dir}})
	if cfg.Persistence.logsQueuePath() != dir {
		t.Errorf("explicit path lost: %q", cfg.Persistence.logsQueuePath())
	}
}

func TestParseByteSize(t *testing.T) {
	for in, want := range map[string]int64{"1024": 1024, "128MiB": 128 << 20, "1g": 1 << 30, "10KB": 10 << 10, "5M": 5 << 20} {
		if got, err := parseByteSize(in); err != nil || got != want {
			t.Errorf("%s = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseByteSize("-1"); err == nil {
		t.Error("negative accepted")
	}
}

func writeAgedBatch(t *testing.T, q *logsQueue, name string, savedAt time.Time, n int) {
	t.Helper()
	if err := os.MkdirAll(q.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(logBatch{Version: logsQueueFileVersion, SavedAt: savedAt, Records: sampleRecords(n)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.dir, name), data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestLogsQueue_AgeSweepDropsOldKeepsYoung(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	q := newLogsQueue(dir, 0, testModuleLogger(t))
	q.now = func() time.Time { return now }
	q.maxAge = 24 * time.Hour
	writeAgedBatch(t, q, "log-00000000000000000001.json", now.Add(-30*time.Hour), 3)
	writeAgedBatch(t, q, "log-00000000000000000002.json", now.Add(-25*time.Hour), 2)
	writeAgedBatch(t, q, "log-00000000000000000003.json", now.Add(-1*time.Hour), 4)
	q.recover()

	before := agentstate.GetOTLPDroppedByReason()["dropped_by_age"]
	if got := q.sweepAged(); got != 5 {
		t.Fatalf("dropped %d, want 5", got)
	}
	if after := agentstate.GetOTLPDroppedByReason()["dropped_by_age"]; after-before != 5 {
		t.Errorf("counter moved by %d, want 5", after-before)
	}
	if n := countQueueFiles(t, dir); n != 1 {
		t.Errorf("files left = %d, want 1", n)
	}
	if recs, _ := q.pending(); recs != 4 {
		t.Errorf("pending records = %d, want 4", recs)
	}

	now = now.Add(24 * time.Hour)
	if got := q.sweepAged(); got != 4 {
		t.Errorf("second sweep dropped %d, want 4", got)
	}
}

func TestLogsQueue_BootSweepAndNoLimit(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-72 * time.Hour)
	seed := newLogsQueue(dir, 0, testModuleLogger(t))
	writeAgedBatch(t, seed, "log-00000000000000000001.json", old, 2)

	q := newLogsQueue(dir, 0, testModuleLogger(t))
	q.setMaxAge(0)
	if n := countQueueFiles(t, dir); n != 1 {
		t.Fatalf("no-limit queue dropped a batch (%d left)", n)
	}

	q2 := newLogsQueue(dir, 0, testModuleLogger(t))
	q2.setMaxAge(24 * time.Hour)
	if n := countQueueFiles(t, dir); n != 0 {
		t.Errorf("boot sweep left %d files", n)
	}
	if recs, _ := q2.pending(); recs != 0 {
		t.Errorf("pending = %d after boot sweep", recs)
	}
}

func TestStrategyStart_DefaultQueueCreated(t *testing.T) {
	clearQueueEnv(t)
	state := t.TempDir()
	t.Setenv("STATE_DIRECTORY", state)
	s := newTestStrategy(t, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()
	if s.logsQueue == nil {
		t.Fatal("logs queue not active by default")
	}
	if _, err := os.Stat(filepath.Join(state, "otlp-queue")); err != nil {
		t.Errorf("queue dir missing: %v", err)
	}
}

func TestStrategyStart_UnwritableDirContinues(t *testing.T) {
	clearQueueEnv(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A path below a regular file can never be created, whatever user runs the tests.
	t.Setenv("STATE_DIRECTORY", blocker)
	s := newTestStrategy(t, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start must not fail: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()
	if s.logsQueue != nil {
		t.Error("queue active on an unwritable directory")
	}
}

func TestStrategyStart_DisabledNoDirCreated(t *testing.T) {
	clearQueueEnv(t)
	state := t.TempDir()
	t.Setenv("STATE_DIRECTORY", state)
	t.Setenv("SENHUB_LOG_QUEUE", "false")
	s := newTestStrategy(t, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()
	if s.logsQueue != nil {
		t.Error("queue active despite SENHUB_LOG_QUEUE=false")
	}
	if _, err := os.Stat(filepath.Join(state, "otlp-queue")); err == nil {
		t.Error("queue dir created while disabled")
	}
}
