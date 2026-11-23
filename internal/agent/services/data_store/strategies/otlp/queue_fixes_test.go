package otlp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskQueue_RecoverRemovesStaleTmpFiles(t *testing.T) {
	dir := t.TempDir()
	q := newWireQueue(metricsQueueKind, dir, 0, testModuleLogger(t))
	if err := q.enqueue([]byte("kept"), 1); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(q.dir, "metric-00000000000000000009.q.tmp")
	if err := os.WriteFile(stale, make([]byte, 4096), 0o640); err != nil {
		t.Fatal(err)
	}
	q2 := newWireQueue(metricsQueueKind, dir, 0, testModuleLogger(t))
	if _, err := os.Stat(stale); err == nil {
		t.Error("a stale .tmp file survived the recovery scan")
	}
	if recs, _ := q2.pending(); recs != 1 {
		t.Errorf("pending = %d, want 1", recs)
	}
}

func TestDiskQueue_RemoveUnreadableFileClearsCounters(t *testing.T) {
	q := newWireQueue(metricsQueueKind, t.TempDir(), 0, testModuleLogger(t))
	if err := q.enqueue([]byte("x"), 5); err != nil {
		t.Fatal(err)
	}
	name := q.files()[0]
	if err := os.WriteFile(filepath.Join(q.dir, name), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	q.removeFile(name)
	if recs, _ := q.pending(); recs != 0 || q.hasFiles() {
		t.Errorf("counters drifted: records=%d hasFiles=%v", recs, q.hasFiles())
	}
}

func TestDiskQueue_EvictSurvivesAFileGoneBehindItsBack(t *testing.T) {
	q := newWireQueue(metricsQueueKind, t.TempDir(), 0, testModuleLogger(t))
	payload := make([]byte, 600)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	if err := q.enqueue(payload, 1); err != nil {
		t.Fatal(err)
	}
	one := q.sizeBytes
	q.maxBytes = 2*one + one/2
	if err := q.enqueue(payload, 1); err != nil {
		t.Fatal(err)
	}
	oldest := q.files()[0]
	if err := os.Remove(filepath.Join(q.dir, oldest)); err != nil {
		t.Fatal(err)
	}
	if err := q.enqueue(payload, 1); err != nil {
		t.Fatal(err)
	}
	if got := len(q.files()); got != 2 {
		t.Errorf("files = %d, want 2: a vanished file must not make the eviction take a live one", got)
	}
}
