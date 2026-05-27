package probes_test

// External-test-package suite (note `package probes_test`, not
// `package probes`). The blank imports below are how every probe
// package's init() — which calls `probes.RegisterProbe(...)` — fires
// when running `go test ./internal/agent/probes/`. The internal
// `package probes` cannot do this itself: a probe package imports
// the `probes` parent for RegisterProbe, so the parent importing the
// probe package back would be a cycle. The external test package is
// the standard Go pattern for breaking that dependency loop while
// keeping the tests close to the code they exercise.

import (
	"sort"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/services/license"

	_ "senhub-agent.go/internal/agent/probes/citrix"
	_ "senhub-agent.go/internal/agent/probes/cpu"
	_ "senhub-agent.go/internal/agent/probes/event"
	_ "senhub-agent.go/internal/agent/probes/gateway"
	_ "senhub-agent.go/internal/agent/probes/host"
	_ "senhub-agent.go/internal/agent/probes/ibmi"
	_ "senhub-agent.go/internal/agent/probes/linuxlogs"
	_ "senhub-agent.go/internal/agent/probes/logicaldisk"
	_ "senhub-agent.go/internal/agent/probes/memory"
	_ "senhub-agent.go/internal/agent/probes/mysql"
	_ "senhub-agent.go/internal/agent/probes/netscaler"
	_ "senhub-agent.go/internal/agent/probes/network"
	_ "senhub-agent.go/internal/agent/probes/postgresql"
	_ "senhub-agent.go/internal/agent/probes/redfish"
	_ "senhub-agent.go/internal/agent/probes/syslog"
	_ "senhub-agent.go/internal/agent/probes/veeam"
	_ "senhub-agent.go/internal/agent/probes/webapp"
)

// TestEveryRegisteredProbeIsAuthorizable enforces a structural
// invariant across the runtime probe registry and the licence
// catalogues — every probe added to the registry MUST also be
// authorizable by at least one licence mechanism:
//
//   - probes package:                                    populated via probes.RegisterProbe
//   - internal/agent/services/license/license.go:        freeTierProbes
//   - internal/agent/services/license/probe_catalog.go:  paidProbes
//
// Without this guard, a probe wired into the registry without
// touching either licence file ships in the binary but no JWT
// licence can authorize it — the validator silently drops the probe
// at runtime even when the licence claims look correct.
//
// When this test fails: add the probe to one of:
//
//   - freeTierProbes (host-local observability, no remote system involved)
//   - paidProbes     (paid probe — claimable by a JWT licence)
func TestEveryRegisteredProbeIsAuthorizable(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()

	// Guard against the silent regression "no probe registered at all".
	// Pre-refactor, the registry was a hardcoded map and the test
	// trivially held when the map was emptied; the self-registration
	// pattern needs an explicit assertion that the catalogue is alive.
	if len(registered) == 0 {
		t.Fatalf("probes.GetRegisteredProbeTypes() returned an empty map — " +
			"no probe package's init() ran. " +
			"Check the blank imports in this test file and in cmd/agent/probes_register.go")
	}

	var orphans []string
	for name := range registered {
		if !license.IsProbeAuthorizable(name) {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("probes registered in registry but neither in freeTierProbes nor paidProbes: %s\n\n"+
			"add each to one of:\n"+
			"  - internal/agent/services/license/license.go (freeTierProbes)        — host-local\n"+
			"  - internal/agent/services/license/probe_catalog.go (paidProbes)      — paid, claimable by JWT",
			strings.Join(orphans, ", "))
	}
}

// TestNoStalePaidProbe enforces the reverse direction: every probe
// claiming a slot in the paid catalogue MUST still be registered.
// A stale entry would let a JWT licence reference a probe that does
// not exist in the binary — confusing to debug, and may leak the
// existence of removed features.
func TestNoStalePaidProbe(t *testing.T) {
	registered := probes.GetRegisteredProbeTypes()

	var stale []string
	for _, name := range license.KnownPaidProbes() {
		if _, ok := registered[name]; !ok {
			stale = append(stale, name)
		}
	}

	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("paid-probe catalogue entries with no matching registered probe: %s\n\n"+
			"either restore the probe registration or remove the entry in probe_catalog.go",
			strings.Join(stale, ", "))
	}
}
