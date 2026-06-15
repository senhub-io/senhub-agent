package agentstate

import "testing"

func TestAgentInstanceID_SetThenGet(t *testing.T) {
	SetAgentInstanceID("abc12345")
	if got := GetAgentInstanceID(); got != "abc12345" {
		t.Errorf("expected abc12345, got %q", got)
	}
	// A later set overwrites (reconfigure / restart of entity emission).
	SetAgentInstanceID("def67890")
	if got := GetAgentInstanceID(); got != "def67890" {
		t.Errorf("expected def67890 after overwrite, got %q", got)
	}
}

func TestAgentInstanceID_EmptyIsEmpty(t *testing.T) {
	// An empty set must read back as "" so a probe source skips the monitors
	// edge rather than emitting an unresolvable From endpoint.
	SetAgentInstanceID("")
	if got := GetAgentInstanceID(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestCollectErrorsCounter(t *testing.T) {
	before := GetCollectErrorsTotal()
	IncrementCollectErrors()
	IncrementCollectErrors()
	IncrementCollectErrors()
	after := GetCollectErrorsTotal()
	if after-before != 3 {
		t.Errorf("expected delta 3, got %d", after-before)
	}
}

func TestProbeCounts_HealthRecorded(t *testing.T) {
	// Reset state
	SetActiveProbes(nil)

	SetActiveProbes([]string{"probe-1", "probe-2", "probe-3", "probe-4"})
	RecordProbeHealth("probe-1", true)
	RecordProbeHealth("probe-2", true)
	RecordProbeHealth("probe-3", false)
	// probe-4 has no recorded health → counts as unknown (not healthy).

	total, healthy := GetProbeCounts()
	if total != 4 {
		t.Errorf("total: got %d, want 4", total)
	}
	if healthy != 2 {
		t.Errorf("healthy: got %d, want 2 (probe-1, probe-2; probe-3=failed; probe-4=unknown)", healthy)
	}
}

func TestProbeCounts_PruneOnReconfig(t *testing.T) {
	SetActiveProbes([]string{"a", "b"})
	RecordProbeHealth("a", true)
	RecordProbeHealth("b", true)

	// Reconfig: only "a" survives.
	SetActiveProbes([]string{"a"})
	total, healthy := GetProbeCounts()
	if total != 1 || healthy != 1 {
		t.Errorf("after reconfig: total=%d healthy=%d, want 1/1", total, healthy)
	}

	// Now "b" comes back, but its old health was pruned — counts as unknown.
	SetActiveProbes([]string{"a", "b"})
	total, healthy = GetProbeCounts()
	if total != 2 {
		t.Errorf("total after re-add: got %d, want 2", total)
	}
	if healthy != 1 {
		t.Errorf("healthy after re-add (b is unknown again): got %d, want 1", healthy)
	}
}

func TestRecordProbeHealth_OverwritesPrevious(t *testing.T) {
	SetActiveProbes([]string{"p"})
	RecordProbeHealth("p", true)
	if _, h := GetProbeCounts(); h != 1 {
		t.Errorf("after ok: healthy=%d, want 1", h)
	}
	RecordProbeHealth("p", false)
	if _, h := GetProbeCounts(); h != 0 {
		t.Errorf("after fail: healthy=%d, want 0", h)
	}
	RecordProbeHealth("p", true)
	if _, h := GetProbeCounts(); h != 1 {
		t.Errorf("recovered: healthy=%d, want 1", h)
	}
}
