package probes

import (
	"sort"

	"senhub-agent.go/internal/agent/services/logger"
)

// A parameter name outlives the code that read it. A probe is rewritten,
// replaced by another implementation, or moved between tiers, and the
// configurations in the field keep the spelling they were written with.
// Nothing rejects them: an unknown key in a probe's params block is
// simply not read, so the option stops working and nobody is told.
//
// That is how the mysql and postgresql probes shipped for a whole cycle
// answering to one set of names while the documentation described
// another (#842). `sslmode: require` produced a plaintext connection.
//
// A probe declares the names it used to answer to here. `agent config
// check` reports them: a name with a replacement is a warning, since the
// configuration still expresses something the agent can do; a name with
// none is an error, because the operator asked for something that will
// not happen.

// LegacyParam describes a parameter name a probe no longer reads under
// that spelling.
type LegacyParam struct {
	// Replacement is the key to write instead. Empty means the option
	// has no equivalent — the probe cannot do that any more.
	Replacement string
	// Note carries what the replacement alone does not say: a change of
	// shape, of unit, or what an operator should do instead when there
	// is nothing to rename to. Optional when Replacement speaks for
	// itself.
	Note string
	// Accepted marks a name the probe still honours as a synonym.
	// Reported as a warning so the configuration converges on one
	// spelling, never as an error — it works today and keeps working.
	Accepted bool
}

var legacyParams = map[string]map[string]LegacyParam{}

// RegisterLegacyParams records the parameter names a probe type used to
// answer to. Called from the probe package's init(), next to
// RegisterProbe, so the list lives with the parser that stopped reading
// them rather than in a table somewhere else that drifts.
//
// A second call for the same probe type merges, so a probe may declare
// its legacy names from more than one file.
func RegisterLegacyParams(probeType string, params map[string]LegacyParam) {
	if probeType == "" || len(params) == 0 {
		return
	}
	existing, ok := legacyParams[probeType]
	if !ok {
		existing = map[string]LegacyParam{}
		legacyParams[probeType] = existing
	}
	for name, p := range params {
		existing[name] = p
	}
}

// LegacyParamsFor returns the legacy names declared for a probe type,
// or nil. The returned map is a copy: callers report on it, they do not
// own it.
func LegacyParamsFor(probeType string) map[string]LegacyParam {
	declared, ok := legacyParams[probeType]
	if !ok {
		return nil
	}
	out := make(map[string]LegacyParam, len(declared))
	for name, p := range declared {
		out[name] = p
	}
	return out
}

// reportLegacyParams logs one line per legacy name a probe was
// configured with. An accepted synonym is worth a line too: the
// configuration works, and the operator should still learn the spelling
// that will be there next year.
func reportLegacyParams(log *logger.ModuleLogger, probeName, probeType string, params map[string]interface{}) {
	if log == nil {
		return
	}
	declared := legacyParams[probeType]
	for _, key := range LegacyParamsUsed(probeType, params) {
		p := declared[key]
		event := log.Warn().
			Str("probe_name", probeName).
			Str("param", key)
		if p.Replacement != "" {
			event = event.Str("use_instead", p.Replacement)
		}
		if p.Note != "" {
			event = event.Str("note", p.Note)
		}
		if p.Accepted {
			event.Msg("probe parameter has been renamed; the old name still works")
			continue
		}
		event.Msg("probe parameter is not read by this probe and has no effect")
	}
}

// LegacyParamsUsed returns the legacy names present in a params block,
// sorted, so a report reads the same way twice.
func LegacyParamsUsed(probeType string, params map[string]interface{}) []string {
	declared := legacyParams[probeType]
	if len(declared) == 0 || len(params) == 0 {
		return nil
	}
	var used []string
	for name := range declared {
		if _, present := params[name]; present {
			used = append(used, name)
		}
	}
	sort.Strings(used)
	return used
}
