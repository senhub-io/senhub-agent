package pushqueue

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestAppendSyncRoundTrip(t *testing.T) {
	q := New[int]("test", 0)

	if err := q.Append([]int{1, 2, 3}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := q.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}

	got := q.Sync()
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("Sync = %v, want [1 2 3] in order", got)
	}
	if n := q.Len(); n != 0 {
		t.Fatalf("Sync left %d items, want an empty queue", n)
	}
}

// TestAbortSyncRestoresOrder pins the property the retry path depends
// on: an undelivered batch goes back at the HEAD, so the order items
// were produced in survives the failure.
func TestAbortSyncRestoresOrder(t *testing.T) {
	q := New[int]("test", 0)

	batch := []int{1, 2}
	_ = q.Append([]int{3, 4})

	if err := q.AbortSync(batch); err != nil {
		t.Fatalf("AbortSync: %v", err)
	}

	got := q.Sync()
	want := []int{1, 2, 3, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sync = %v, want %v", got, want)
		}
	}
}

// TestCapDropsOldestAndCounts is the reason this package exists. The
// items dropped must be the OLDEST, and the loss must be counted under
// the sink's own name — a silent trim is indistinguishable from a sink
// that is keeping up.
func TestCapDropsOldestAndCounts(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()

	q := New[int]("capped-sink", 3)
	if err := q.Append([]int{1, 2, 3, 4, 5}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if got := q.Len(); got != 3 {
		t.Fatalf("Len = %d, want the cap of 3", got)
	}

	got := q.Sync()
	want := []int{3, 4, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sync = %v, want %v (oldest dropped first)", got, want)
		}
	}

	if n := agentstate.GetPushBufferDropped()["capped-sink"]; n != 2 {
		t.Errorf("dropped counter = %d, want 2", n)
	}
}

// TestSustainedOutageStaysBounded is the outage-recovery scenario: a
// sink that cannot deliver keeps re-queueing its batch while collection
// keeps appending. Before the cap this grew until the process died
// (#267); the queue must plateau at the cap, and the counter must
// account for every item it shed.
func TestSustainedOutageStaysBounded(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()

	const cap = 100
	q := New[int]("outage-sink", cap)

	produced := 0
	for cycle := 0; cycle < 200; cycle++ {
		batch := make([]int, 10)
		for i := range batch {
			batch[i] = produced
			produced++
		}
		if err := q.Append(batch); err != nil {
			t.Fatalf("Append: %v", err)
		}
		// The sink is down: take the batch, fail, hand it back.
		if err := q.AbortSync(q.Sync()); err != nil {
			t.Fatalf("AbortSync: %v", err)
		}
		if n := q.Len(); n > cap {
			t.Fatalf("cycle %d: queue grew past the cap: %d > %d", cycle, n, cap)
		}
	}

	// Recovery: the sink comes back and drains what survived.
	survivors := q.Sync()
	if len(survivors) != cap {
		t.Fatalf("after recovery the queue drained %d items, want the cap of %d", len(survivors), cap)
	}
	// What survived is the freshest end of the stream, which is the
	// half of the trade-off that makes shedding acceptable.
	if survivors[len(survivors)-1] != produced-1 {
		t.Errorf("newest item = %d, want %d — the cap shed the wrong end",
			survivors[len(survivors)-1], produced-1)
	}

	dropped := agentstate.GetPushBufferDropped()["outage-sink"]
	if int(dropped) != produced-cap {
		t.Errorf("dropped counter = %d, want %d (produced %d, kept %d)",
			dropped, produced-cap, produced, cap)
	}

	if n := q.Len(); n != 0 {
		t.Errorf("queue not drained after recovery: %d items left", n)
	}
}
