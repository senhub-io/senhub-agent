package otlp

import (
	"bytes"
	"strings"
	"testing"
)

// A queue that evicts on every write warns once per interval with the
// totals, not once per evicted batch.
func TestDiskQueue_EvictionWarnsOncePerSweepWithTotals(t *testing.T) {
	var buf lockedBuffer
	q := newWireQueue(tracesQueueKind, t.TempDir(), 300, capturedLogger(&buf))
	before := droppedBy("traces_queue_full")

	payload := bytes.Repeat([]byte("span-"), 60)
	const writes = 40
	for i := 0; i < writes; i++ {
		if err := q.enqueue(payload, 1); err != nil {
			t.Fatal(err)
		}
	}
	evicted := int(droppedBy("traces_queue_full") - before)
	if evicted < 10 {
		t.Fatalf("only %d evictions, the test needs many", evicted)
	}
	if n := strings.Count(buf.String(), "over size cap"); n != 1 {
		t.Fatalf("%d eviction warnings for %d evictions, want 1:\n%s", n, evicted, buf.String())
	}
	// The sweep reports what the first warning left out.
	q.sweepAged()
	if n := strings.Count(buf.String(), "over size cap"); n != 2 {
		t.Errorf("%d warnings after the sweep, want 2", n)
	}
	q.sweepAged()
	if n := strings.Count(buf.String(), "over size cap"); n != 2 {
		t.Errorf("an empty sweep warned again (%d)", n)
	}
}
