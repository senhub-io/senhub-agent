package otlp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func TestCheckpointer_DisabledWithEmptyPath(t *testing.T) {
	c := newCheckpointer(checkpointConfig{Path: "", Interval: time.Second}, newMetricStore(), nil)
	if c.enabled() {
		t.Error("empty path should mean disabled")
	}
	// save/load on a disabled checkpointer are silent no-ops.
	if err := c.save(); err != nil {
		t.Errorf("save on disabled: %v", err)
	}
	if n, err := c.loadAndRestore(); err != nil || n != 0 {
		t.Errorf("loadAndRestore on disabled: n=%d err=%v, want 0, nil", n, err)
	}
}

func TestCheckpointer_SaveAndRestoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := newMetricStore()
	store.upsert(datapoint.DataPoint{
		Name:      "metric_a",
		Value:     1.5,
		Timestamp: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "t"},
			{Key: "host", Value: "h"},
		},
	})
	store.upsert(datapoint.DataPoint{
		Name:      "metric_b",
		Value:     2.5,
		Timestamp: time.Date(2026, 5, 18, 12, 0, 1, 0, time.UTC),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "t"},
		},
	})

	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, snapshotFileName)); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}

	// Restore into a fresh store and check both series came back.
	fresh := newMetricStore()
	c2 := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, fresh, nil)
	n, err := c2.loadAndRestore()
	if err != nil {
		t.Fatalf("loadAndRestore: %v", err)
	}
	if n != 2 {
		t.Errorf("restored count=%d, want 2", n)
	}
	if got := fresh.size(); got != 2 {
		t.Errorf("fresh store size=%d, want 2", got)
	}
	if got := fresh.probeSeriesCount("p"); got != 2 {
		t.Errorf("probe count for p = %d, want 2", got)
	}
}

func TestCheckpointer_LoadMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := newMetricStore()
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
	n, err := c.loadAndRestore()
	if err != nil {
		t.Errorf("loadAndRestore on empty dir: %v", err)
	}
	if n != 0 {
		t.Errorf("expected n=0 on missing file, got %d", n)
	}
}

func TestCheckpointer_LoadCorruptFileWarnsAndStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, snapshotFileName), []byte("not json {{{"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	store := newMetricStore()
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)

	before := agentstate.GetOTLPCheckpointErrorsByStage()["parse"]
	n, err := c.loadAndRestore()
	if err != nil {
		t.Errorf("loadAndRestore on corrupt: should swallow err, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected n=0 on corrupt, got %d", n)
	}
	after := agentstate.GetOTLPCheckpointErrorsByStage()["parse"]
	if after != before+1 {
		t.Errorf("parse error counter: before=%d after=%d, want +1", before, after)
	}
	if got := store.size(); got != 0 {
		t.Errorf("store should be empty after corrupt restore, got %d", got)
	}
}

func TestCheckpointer_LoadVersionMismatchSkips(t *testing.T) {
	dir := t.TempDir()
	bad := checkpoint{Version: 999, SavedAt: time.Now()}
	data, _ := json.Marshal(&bad)
	if err := os.WriteFile(filepath.Join(dir, snapshotFileName), data, 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	store := newMetricStore()
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)

	before := agentstate.GetOTLPCheckpointErrorsByStage()["version_mismatch"]
	n, err := c.loadAndRestore()
	if err != nil {
		t.Errorf("version mismatch should not be an error to caller, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected n=0 on version mismatch, got %d", n)
	}
	after := agentstate.GetOTLPCheckpointErrorsByStage()["version_mismatch"]
	if after != before+1 {
		t.Errorf("version_mismatch counter: before=%d after=%d, want +1", before, after)
	}
}

func TestCheckpointer_SaveAtomicViaRename(t *testing.T) {
	dir := t.TempDir()
	store := newMetricStore()
	store.upsert(datapoint.DataPoint{
		Name:  "m",
		Value: 1,
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "p"},
			{Key: "probe_type", Value: "t"},
		},
	})
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, store, nil)
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// No stray .tmp left behind after a successful save.
	if _, err := os.Stat(filepath.Join(dir, snapshotTmpName)); !os.IsNotExist(err) {
		t.Errorf("expected no .tmp file after successful save, stat err=%v", err)
	}

	// File is parseable JSON of the expected shape.
	data, err := os.ReadFile(filepath.Join(dir, snapshotFileName))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var got checkpoint
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if got.Version != checkpointFormatVersion {
		t.Errorf("version=%d, want %d", got.Version, checkpointFormatVersion)
	}
	if len(got.Entries) != 1 {
		t.Errorf("entries=%d, want 1", len(got.Entries))
	}
}

func TestCheckpointer_StopWithoutStartIsSafe(t *testing.T) {
	dir := t.TempDir()
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, newMetricStore(), nil)
	done := make(chan struct{})
	go func() {
		c.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop() deadlocked when start() was never called")
	}
}

func TestCheckpointer_StopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	c := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, newMetricStore(), nil)
	c.start(t.Context())
	c.stop()
	done := make(chan struct{})
	go func() {
		c.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second stop() deadlocked")
	}
}

// TestCheckpointer_RestoredZombiesEvictedByStaleness is the #308
// acceptance test: a checkpoint seeded with a series whose producer no
// longer exists is restored, and the staleness eviction removes it
// within one cycle, while a fresh series survives. Before the fix the
// restored entry re-exported forever with fresh timestamps.
func TestCheckpointer_RestoredZombiesEvictedByStaleness(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	// Seed and persist a store holding one dead series (last datapoint
	// an hour ago — its probe was removed) and one fresh series.
	seed := newMetricStore()
	seed.upsert(datapoint.DataPoint{
		Name: "senhub.ibmi.cpu", Value: 1, Timestamp: now.Add(-time.Hour),
		Tags: []tags.Tag{{Key: "probe_name", Value: "removed-probe"}, {Key: "probe_type", Value: "ibmi"}},
	})
	seed.upsert(datapoint.DataPoint{
		Name: "system.cpu.utilization", Value: 0.4, Timestamp: now.Add(-30 * time.Second),
		Tags: []tags.Tag{{Key: "probe_name", Value: "cpu"}, {Key: "probe_type", Value: "cpu"}},
	})
	cw := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, seed, nil)
	if err := cw.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Restart: a fresh store restores the snapshot — both entries come
	// back, original observation times preserved.
	restored := newMetricStore()
	cr := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, restored, nil)
	if n, err := cr.loadAndRestore(); err != nil || n != 2 {
		t.Fatalf("loadAndRestore = %d, %v; want 2 entries", n, err)
	}

	// One push-cycle eviction pass: the zombie goes, the live one stays.
	if n := restored.evictStale(now, 10*time.Minute); n != 1 {
		t.Fatalf("evicted %d, want exactly the zombie", n)
	}
	metrics, _ := restored.snapshot()
	if len(metrics) != 1 || metrics[0].MetricName != "system.cpu.utilization" {
		t.Fatalf("post-restart survivors = %+v, want only the live series", metrics)
	}

	// The next checkpoint save persists the shrunken store: the zombie
	// does not resurrect on the following restart.
	if err := cr.save(); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	again := newMetricStore()
	ca := newCheckpointer(checkpointConfig{Path: dir, Interval: time.Hour}, again, nil)
	if n, _ := ca.loadAndRestore(); n != 1 {
		t.Fatalf("second restore = %d entries, want 1 (zombie gone from disk)", n)
	}
}
