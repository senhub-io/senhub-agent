package agentstate

import "testing"

func TestStrategyFailures_RecordClearPrune(t *testing.T) {
	ResetStrategyFailuresForTest()
	t.Cleanup(ResetStrategyFailuresForTest)

	if got := len(GetStrategyFailures()); got != 0 {
		t.Fatalf("baseline not empty: %d", got)
	}

	RecordStrategyFailure("otlp", StrategyFailureInvalidConfig, "Authorization Bearer but no credential")
	RecordStrategyFailure("event", StrategyFailureStart, "listen: address in use")

	failures := GetStrategyFailures()
	if len(failures) != 2 {
		t.Fatalf("got %d failures, want 2", len(failures))
	}
	if failures["otlp"].Reason != StrategyFailureInvalidConfig {
		t.Errorf("otlp reason=%q", failures["otlp"].Reason)
	}

	// A strategy that starts on the next refresh stops being reported.
	ClearStrategyFailure("otlp")
	if _, still := GetStrategyFailures()["otlp"]; still {
		t.Error("otlp still reported as failing after ClearStrategyFailure")
	}

	// A strategy removed from the configuration stops being reported too,
	// otherwise deleting a broken fragment alerts forever.
	PruneStrategyFailures([]string{"http"})
	if got := len(GetStrategyFailures()); got != 0 {
		t.Errorf("prune left %d failures, want 0", got)
	}

	// The snapshot is a copy.
	RecordStrategyFailure("otlp", StrategyFailureStart, "boom")
	snap := GetStrategyFailures()
	delete(snap, "otlp")
	if _, ok := GetStrategyFailures()["otlp"]; !ok {
		t.Error("mutating the snapshot leaked into the live state")
	}
}

func TestRecordStrategyFailure_NormalisesEmptyValues(t *testing.T) {
	ResetStrategyFailuresForTest()
	t.Cleanup(ResetStrategyFailuresForTest)

	RecordStrategyFailure("", "", "detail")
	f, ok := GetStrategyFailures()["unknown"]
	if !ok {
		t.Fatal("empty name was not normalised to \"unknown\"")
	}
	if f.Reason != "unknown" {
		t.Errorf("reason=%q, want unknown", f.Reason)
	}
}
