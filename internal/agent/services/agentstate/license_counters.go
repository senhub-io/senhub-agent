package agentstate

import "sync"

// licenseInvalid latches the current licence-rejection state, labelled by
// reason (validation_failed, binding_mismatch, expired_no_grace). A configured
// licence that the validator rejects silently drops the agent to free tier;
// this gauge makes that visible so monitoring can alert instead of the failure
// hiding in a single WARN line (#486). 1 = currently rejected for that reason,
// 0 = not (cleared when a licence validates).
var licenseInvalid = struct {
	mu sync.Mutex
	m  map[string]uint64
}{m: map[string]uint64{}}

// SetLicenseInvalid latches one rejection reason to 1. Idempotent.
func SetLicenseInvalid(reason string) {
	licenseInvalid.mu.Lock()
	licenseInvalid.m[reason] = 1
	licenseInvalid.mu.Unlock()
}

// ClearLicenseInvalid resets all rejection reasons to 0. Called when a licence
// validates successfully so a recovered host stops alerting.
func ClearLicenseInvalid() {
	licenseInvalid.mu.Lock()
	for k := range licenseInvalid.m {
		licenseInvalid.m[k] = 0
	}
	licenseInvalid.mu.Unlock()
}

// GetLicenseInvalidByReason returns a copy of the per-reason gauge values.
func GetLicenseInvalidByReason() map[string]uint64 {
	licenseInvalid.mu.Lock()
	defer licenseInvalid.mu.Unlock()
	out := make(map[string]uint64, len(licenseInvalid.m))
	for k, v := range licenseInvalid.m {
		out[k] = v
	}
	return out
}

// ResetLicenseInvalidForTest clears the gauge map. Test-only.
func ResetLicenseInvalidForTest() {
	licenseInvalid.mu.Lock()
	licenseInvalid.m = map[string]uint64{}
	licenseInvalid.mu.Unlock()
}
