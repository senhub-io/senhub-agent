package probes

import (
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/license"
)

// TestEveryRegisteredProbeIsAuthorizable enforces a structural invariant
// across three files that have drifted in the past — every probe added
// to the registry MUST also be authorizable by at least one license
// mechanism:
//
//   - this file:                                       probeConstructors
//   - internal/agent/services/license/license.go:      freeTierProbes
//   - internal/agent/services/license/compact.go:      probeBitmap
//
// Without this guard, a probe added to the registry without touching
// either license file ships in the binary but the compact license
// format cannot authorize it — only the longer JWT format works, which
// is the inverse of why the compact format exists.
//
// When this test fails: add the probe to one of:
//
//   - freeTierProbes  (host-local observability, no remote system involved)
//   - probeBitmap     (paid probe — claim the next free bit, 13 left at
//                      the time of writing)
//
// Reserved bits (slots left empty in the bitmap as a comment) are fine —
// they do not appear in the map until claimed.
func TestEveryRegisteredProbeIsAuthorizable(t *testing.T) {
	var orphans []string
	for name := range probeConstructors {
		if !license.IsProbeAuthorizable(name) {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("probes registered in registry.go but neither in freeTierProbes nor probeBitmap: %s\n\n"+
			"add each to one of:\n"+
			"  - internal/agent/services/license/license.go (freeTierProbes)  — host-local\n"+
			"  - internal/agent/services/license/compact.go (probeBitmap)     — paid, claim next free bit",
			strings.Join(orphans, ", "))
	}
}

// TestNoStaleBitmapEntry enforces the reverse direction: every probe
// claiming a bit in the compact-license bitmap MUST still be registered
// in this file. A stale entry would let a customer's compact license
// grant access to a probe that does not exist in the binary —
// confusing to debug, and may leak the existence of removed features.
//
// Reserved bits (no map entry, just a comment in compact.go) are fine.
// What this catches is e.g. "redfish: 11" pointing at a removed probe.
func TestNoStaleBitmapEntry(t *testing.T) {
	var stale []string
	for _, name := range license.CompactBitmapProbeNames() {
		if _, ok := probeConstructors[name]; !ok {
			stale = append(stale, name)
		}
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("compact-license probeBitmap entries with no matching probe in registry.go: %s\n\n"+
			"either restore the probe in registry.go or remove the bitmap entry in compact.go\n"+
			"(leave the bit reserved via a comment if the slot may be reused later)",
			strings.Join(stale, ", "))
	}
}
