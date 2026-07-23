package agentstate

import "testing"

func TestOTLPReceiverIngestedCounter(t *testing.T) {
	ResetOTLPReceiverCountersForTest()
	t.Cleanup(ResetOTLPReceiverCountersForTest)

	IncrementOTLPReceiverIngested("metrics", 3)
	IncrementOTLPReceiverIngested("metrics", 2)
	IncrementOTLPReceiverIngested("logs", 4)
	IncrementOTLPReceiverIngested("traces", 0) // ignored
	IncrementOTLPReceiverIngested("", 5)       // ignored

	got := GetOTLPReceiverIngestedBySignal()
	if got["metrics"] != 5 {
		t.Errorf("metrics = %d, want 5", got["metrics"])
	}
	if got["logs"] != 4 {
		t.Errorf("logs = %d, want 4", got["logs"])
	}
	if _, ok := got["traces"]; ok {
		t.Errorf("traces should be absent (0 ignored), got %d", got["traces"])
	}
}

func TestOTLPReceiverDroppedCounter(t *testing.T) {
	ResetOTLPReceiverCountersForTest()
	t.Cleanup(ResetOTLPReceiverCountersForTest)

	IncrementOTLPReceiverDropped("logs", "no_sink", 2)
	IncrementOTLPReceiverDropped("traces", "no_sink", 7)
	IncrementOTLPReceiverDropped("metrics", "unmapped", 1)
	IncrementOTLPReceiverDropped("logs", "", 9) // ignored (empty reason)

	got := GetOTLPReceiverDroppedBySignal()
	if got[otlpReceiverDropKey{Signal: "logs", Reason: "no_sink"}] != 2 {
		t.Errorf("logs/no_sink = %d, want 2", got[otlpReceiverDropKey{Signal: "logs", Reason: "no_sink"}])
	}
	if got[otlpReceiverDropKey{Signal: "traces", Reason: "no_sink"}] != 7 {
		t.Errorf("traces/no_sink = %d, want 7", got[otlpReceiverDropKey{Signal: "traces", Reason: "no_sink"}])
	}
	if got[otlpReceiverDropKey{Signal: "metrics", Reason: "unmapped"}] != 1 {
		t.Errorf("metrics/unmapped = %d, want 1", got[otlpReceiverDropKey{Signal: "metrics", Reason: "unmapped"}])
	}
	if _, ok := got[otlpReceiverDropKey{Signal: "logs", Reason: ""}]; ok {
		t.Error("empty reason should be ignored")
	}
}
