package agentstate

import "sync"

// Known licence-rejection reasons for the senhub.agent.license.invalid gauge.
const (
	LicenseReasonValidationFailed = "validation_failed"
	LicenseReasonBindingMismatch  = "binding_mismatch"
	LicenseReasonExpiredNoGrace   = "expired_no_grace"
)

// licenseKnownReasons lists every known rejection reason so ClearLicenseInvalid
// can publish 0 for each even before any rejection was latched.
var licenseKnownReasons = []string{
	LicenseReasonValidationFailed,
	LicenseReasonBindingMismatch,
	LicenseReasonExpiredNoGrace,
}

// licenseInvalid latches the current licence-rejection state, labelled by
// reason. A configured licence that the validator rejects silently drops the
// agent to free tier; this gauge makes that visible (#486). 1 = currently
// rejected for that reason, 0 = not (cleared when a licence validates).
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

// ClearLicenseInvalid publishes all known reasons at 0 (creating keys if
// absent) and resets any other latched key to 0. Called when a licence
// validates so a recovered host stops alerting.
func ClearLicenseInvalid() {
	licenseInvalid.mu.Lock()
	for _, r := range licenseKnownReasons {
		licenseInvalid.m[r] = 0
	}
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
