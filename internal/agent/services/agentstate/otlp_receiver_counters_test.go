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

// TestSeedOTLPReceiverIngested is the regression for #688: a configured but
// idle receiver must expose its ingest self-metric at 0, and seeding must never
// reset an already-accumulated count.
func TestSeedOTLPReceiverIngested(t *testing.T) {
	ResetOTLPReceiverCountersForTest()
	t.Cleanup(ResetOTLPReceiverCountersForTest)

	SeedOTLPReceiverIngested("metrics", "logs", "")
	got := GetOTLPReceiverIngestedBySignal()
	if v, ok := got["metrics"]; !ok || v != 0 {
		t.Errorf("metrics seeded = (%d,%v), want (0,true)", v, ok)
	}
	if v, ok := got["logs"]; !ok || v != 0 {
		t.Errorf("logs seeded = (%d,%v), want (0,true)", v, ok)
	}
	if _, ok := got[""]; ok {
		t.Error("empty signal must not be seeded")
	}
	if _, ok := got["traces"]; ok {
		t.Error("un-seeded signal must stay absent")
	}

	// A later ingest raises the seeded counter; re-seeding must not clobber it.
	IncrementOTLPReceiverIngested("metrics", 4)
	SeedOTLPReceiverIngested("metrics")
	if v := GetOTLPReceiverIngestedBySignal()["metrics"]; v != 4 {
		t.Errorf("metrics after ingest+reseed = %d, want 4 (seed must not reset)", v)
	}
}
