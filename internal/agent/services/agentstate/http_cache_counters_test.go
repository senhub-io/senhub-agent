package agentstate

import "testing"

func TestHTTPCacheDropped_PerReasonCounting(t *testing.T) {
	ResetHTTPCacheDroppedForTest()
	t.Cleanup(ResetHTTPCacheDroppedForTest)

	IncrementHTTPCacheDropped("http_cache_cap")
	IncrementHTTPCacheDropped("http_cache_cap")
	IncrementHTTPCacheDropped("") // becomes "unknown"

	got := GetHTTPCacheDroppedByReason()
	if got["http_cache_cap"] != 2 {
		t.Errorf("http_cache_cap: got %d, want 2", got["http_cache_cap"])
	}
	if got["unknown"] != 1 {
		t.Errorf("unknown: got %d, want 1", got["unknown"])
	}

	// Snapshot must be a copy — mutating it must not affect the source.
	got["http_cache_cap"] = 99
	if again := GetHTTPCacheDroppedByReason(); again["http_cache_cap"] != 2 {
		t.Errorf("snapshot mutation leaked into source: got %d, want 2", again["http_cache_cap"])
	}
}
