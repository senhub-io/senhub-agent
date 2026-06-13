package license

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
	"cpu":                  true,
	"memory":               true,
	"logicaldisk":          true,
	"network":              true,
	"ping_gateway":         true,
	"ping_webapp":          true,
	"load_webapp":          true,
	"wifi_signal_strength": true,
	"syslog":               true,
	"event":                true,
	"redfish":              true,
	"citrix":               true,
	"netscaler":            true,
	"veeam":                true,
	"mysql":                true,
	"postgresql":           true,
	"linux_logs":           true,
	"filetail":             true,
	"ibmi":                 true,
	// redis is FREE (open-source target — see freeTierProbes in license.go).
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
