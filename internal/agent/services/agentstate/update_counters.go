package agentstate

import "sync"

// updateRejected counts self-update attempts refused by the artifact
// verification layer, labelled by reason (signature_unavailable,
// signature_invalid). A nonzero value on a fleet host is either a
// mis-published release or an attempted supply-chain tamper — both
// need eyes (#266).
var updateRejected = struct {
	mu sync.Mutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// IncrementUpdateRejected records one refused self-update attempt.
func IncrementUpdateRejected(reason string) {
	updateRejected.mu.Lock()
	updateRejected.m[reason]++
	updateRejected.mu.Unlock()
}

// GetUpdateRejectedByReason returns a copy of the per-reason counts.
func GetUpdateRejectedByReason() map[string]uint64 {
	updateRejected.mu.Lock()
	defer updateRejected.mu.Unlock()
	out := make(map[string]uint64, len(updateRejected.m))
	for k, v := range updateRejected.m {
		out[k] = v
	}
	return out
}

// ResetUpdateRejectedForTest clears the counters. Test-only.
func ResetUpdateRejectedForTest() {
	updateRejected.mu.Lock()
	updateRejected.m = map[string]uint64{}
	updateRejected.mu.Unlock()
}
