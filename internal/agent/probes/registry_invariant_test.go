package probes

import (
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/license"
)

// TestEveryRegisteredProbeIsAuthorizable enforces a structural invariant
// across two files that have drifted in the past — every probe added
// to the registry MUST also be authorizable by at least one licence
// mechanism:
//
//   - this file:                                       probeConstructors
//   - internal/agent/services/license/license.go:      freeTierProbes
//   - internal/agent/services/license/probe_catalog.go: paidProbes
//
// Without this guard, a probe added to the registry without touching
// either licence file ships in the binary but no JWT licence can
// authorize it — the validator silently drops the probe at runtime
// even when the licence claims look correct.
//
// When this test fails: add the probe to one of:
//
//   - freeTierProbes (host-local observability, no remote system involved)
//   - paidProbes     (paid probe — claimable by a JWT licence)
func TestEveryRegisteredProbeIsAuthorizable(t *testing.T) {
	var orphans []string
	for name := range probeConstructors {
		if !license.IsProbeAuthorizable(name) {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("probes registered in registry.go but neither in freeTierProbes nor paidProbes: %s\n\n"+
			"add each to one of:\n"+
			"  - internal/agent/services/license/license.go (freeTierProbes)        — host-local\n"+
			"  - internal/agent/services/license/probe_catalog.go (paidProbes)      — paid, claimable by JWT",
			strings.Join(orphans, ", "))
	}
}

// TestNoStalePaidProbe enforces the reverse direction: every probe
// claiming a slot in the paid catalogue MUST still be registered in
// this file. A stale entry would let a JWT licence reference a probe
// that does not exist in the binary — confusing to debug, and may
// leak the existence of removed features.
func TestNoStalePaidProbe(t *testing.T) {
	var stale []string
	for _, name := range license.KnownPaidProbes() {
		if _, ok := probeConstructors[name]; !ok {
			stale = append(stale, name)
		}
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("paid-probe catalogue entries with no matching probe in registry.go: %s\n\n"+
			"either restore the probe in registry.go or remove the entry in probe_catalog.go",
			strings.Join(stale, ", "))
	}
}
