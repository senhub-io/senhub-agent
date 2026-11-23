package otlp

import (
	"os"
	"testing"
	"time"
)

// At stop with the backend up, a live push must not overtake what waits on disk.
func TestMetricsQueue_StoppingDoesNotOvertakeBacklog(t *testing.T) {
	for _, proto := range wireProtocols {
		t.Run(proto, func(t *testing.T) {
			col := newFakeCollector(t, proto)
			rig := newWireRig(t, col, t.TempDir(), "metrics", 0)
			col.setDown(true)
			_ = rig.pushMetric(1, 1)
			col.setDown(false)
			rig.ch.markSuccess()
			rig.ch.beginShutdown(time.Second)
			if rig.ch.relayAdmit() != relayKeep {
				t.Error("a relay must keep its batch behind the backlog at stop")
			}
			if err := rig.pushMetric(2, 1); err == nil {
				t.Fatal("want the push kept behind the backlog")
			}
			if got := len(col.receivedMetrics()); got != 0 {
				t.Errorf("%d requests overtook the backlog", got)
			}
			if rig.files() != 2 {
				t.Errorf("files = %d, want 2", rig.files())
			}
		})
	}
}

// A request that cannot be kept is counted and warned about, not silently lost.
func TestWireChannel_UnkeepableRequestIsCounted(t *testing.T) {
	dir := t.TempDir()
	q := newWireQueue(metricsQueueKind, dir, 0, testModuleLogger(t))
	if err := os.WriteFile(q.dir, []byte("a file where the directory should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := newWireChannel("metrics", q, true, false, testModuleLogger(t))
	before := droppedBy("queue_write_failed")
	if ch.keepRelayed(func() ([]byte, error) { return []byte("x"), nil }, 4) {
		t.Fatal("kept despite an unwritable queue")
	}
	if got := droppedBy("queue_write_failed") - before; got != 4 {
		t.Errorf("queue_write_failed counted %d, want 4", got)
	}
}
