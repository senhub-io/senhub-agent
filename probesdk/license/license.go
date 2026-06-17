// Package license is the public mirror of the parts of the licence
// catalogue an out-of-module test needs (senhub-agent.go/internal/agent/services/license).
// The validator and the catalogue stay in core; this only re-exports the
// read-only lookups so the enterprise repo can assert structural
// invariants (every paid probe it ships is in the catalogue, and vice
// versa) without importing internal/.
package license

import ilicense "senhub-agent.go/internal/agent/services/license"

// KnownPaidProbes returns the probe names a JWT licence is allowed to
// grant (the paid-probe catalogue).
func KnownPaidProbes() []string {
	return ilicense.KnownPaidProbes()
}

// GetFreeTierProbes returns the host-local probes that need no licence.
func GetFreeTierProbes() []string {
	return ilicense.GetFreeTierProbes()
}

// IsProbeAuthorizable reports whether a probe can be authorized by some
// licence mechanism — free tier or the paid catalogue.
func IsProbeAuthorizable(probeName string) bool {
	return ilicense.IsProbeAuthorizable(probeName)
}
