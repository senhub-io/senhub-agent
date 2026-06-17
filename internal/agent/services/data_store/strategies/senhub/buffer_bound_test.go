package senhub

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func mkPoints(n int, name string) []datapoint.DataPoint {
	out := make([]datapoint.DataPoint, n)
	for i := range out {
		out[i] = datapoint.DataPoint{Name: name, Value: float64(i)}
	}
	return out
}

// TestBuffer_OutageStaysBounded simulates an intake outage (#267,
// audit A3): collection keeps appending while every sync fails and
// re-prepends its batch. Before the cap this grew without bound until
// OOM; now the buffer stays flat at the cap and drops are counted.
func TestBuffer_OutageStaysBounded(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	t.Cleanup(agentstate.ResetPushBufferDroppedForTest)

	b := NewBufferWithCap(1000)
	for cycle := 0; cycle < 50; cycle++ {
		if err := b.Append(mkPoints(100, "m")); err != nil {
			t.Fatalf("append: %v", err)
		}
		batch := b.Sync()
		// Intake down: the whole batch comes back.
		if err := b.AbortSync(batch); err != nil {
			t.Fatalf("abort: %v", err)
		}
	}
	final := b.Sync()
	if len(final) != 1000 {
		t.Fatalf("buffer length = %d after outage, want flat at cap 1000", len(final))
	}
	drops := agentstate.GetPushBufferDropped()["senhub"]
	if drops != 4000 { // 5000 appended - 1000 retained
		t.Errorf("dropped = %d, want 4000", drops)
	}
	// Drop-oldest: the survivors are the newest points.
	if final[len(final)-1].Value != 99 {
		t.Errorf("newest point missing after trims: %+v", final[len(final)-1])
	}
}

// TestBuffer_UnboundedWhenZero pins the 0 = unbounded contract.
func TestBuffer_UnboundedWhenZero(t *testing.T) {
	agentstate.ResetPushBufferDroppedForTest()
	t.Cleanup(agentstate.ResetPushBufferDroppedForTest)

	b := NewBufferWithCap(0)
	if err := b.Append(mkPoints(5000, "m")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := len(b.Sync()); got != 5000 {
		t.Errorf("len = %d, want 5000 (unbounded)", got)
	}
	if n := agentstate.GetPushBufferDropped()["senhub"]; n != 0 {
		t.Errorf("drops = %d, want 0", n)
	}
}
