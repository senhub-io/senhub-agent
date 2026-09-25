package license

import "fmt"

// Catalog of probes known to the licence system. Free-tier probes live
// in `freeTierProbes` (see license.go) and need no licence at all.
// Every other probe MUST appear in `paidProbes` for the validator to
// recognise it as a name a JWT licence is allowed to grant.
//
// Adding a probe here is one of the four required touch-points
// documented in `.claude/rules/probes.md` ("License touch-points —
// every new probe MUST update all four"). The structural test
// `TestEveryRegisteredProbeIsAuthorizable` in
// `internal/agent/probes/registry_invariant_test.go` fails CI if a
// new probe is wired in the registry without claiming a slot here.
//
// The catalogue replaced the previous compact-license bitmap. The
// compact format relied on a hardcoded HMAC secret that did not
// survive open-sourcing the agent; the catalogue keeps the
// "registered probe" semantics that other code depends on, without
// the broken cryptography. See docs/LICENSE-SYSTEM.md for the JWT
// scheme that replaced it.

var paidProbes = map[string]bool{
	// Deep vendor infra integrations with no credible free exporter.
	"citrix":    true,
	"netscaler": true,
	"veeam":     true,
	"redfish":   true,
	"ibmi":      true,
	// Dell PowerStore storage-array health (REST API): cluster state,
	// hardware faults, capacity, volumes, active alerts.
	"powerstore": true,
	// Pro-tier HA / cloud probes (enterprise build): deep database, hypervisor
	// and Microsoft-cloud integrations.
	"mssql_ha":          true,
	"oracle_enterprise": true,
	"hyperv_ha":         true,
	"vsphere_ha":        true,
	"ad_hybrid":         true,
	"exchange_online":   true,
	// Azure Container Apps console log stream, read through ARM.
	"azure_container_apps": true,
	// The same subscription's jobs: the verdict, the duration and the
	// output of each execution, read once it has finished.
	"azure_container_app_jobs": true,
	// Bespoke commercial collector: third-party apps push events over HTTP.
	"event": true,
	// Active / synthetic checks.
	"ping_gateway": true,
	"ping_webapp":  true,
	"load_webapp":  true,
}

// IsKnownPaidProbe reports a probe type the paid catalogue names, whether
// or not this build carries it.
func IsKnownPaidProbe(name string) bool { return paidProbes[name] }

// NotInThisBuild is the message for a configured probe type the binary
// does not contain. For a type of the paid catalogue it names the
// edition that ships it: a licence cannot help an open-source build that
// does not carry the probe's code, and saying "requires a valid licence"
// sent an operator to buy the wrong thing.
func NotInThisBuild(probeType string) string {
	if paidProbes[probeType] {
		return fmt.Sprintf("probe type %q is not part of this build: it ships in the SenHub Agent full edition", probeType)
	}
	return fmt.Sprintf("unknown probe type %q", probeType)
}

// KnownPaidProbes returns the names of every probe registered as
// authorizable by a paid licence. Used by the structural invariant
// test to detect catalogue entries with no matching probe in the
// registry (and vice-versa).
func KnownPaidProbes() []string {
	names := make([]string, 0, len(paidProbes))
	for name := range paidProbes {
		names = append(names, name)
	}
	return names
}
