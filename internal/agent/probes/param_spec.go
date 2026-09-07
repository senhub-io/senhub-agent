package probes

import "senhub-agent.go/internal/agent/probes/spec"

// The parameter schema lives in the leaf package probes/spec, which
// imports only the standard library, so the HTTP strategy can serve it
// without importing this package (which imports data_store). Probe
// packages keep declaring through these aliases, next to RegisterProbe.

type (
	ParamKind   = spec.ParamKind
	ParamSpec   = spec.ParamSpec
	ProbeSpec   = spec.Probe
	SpecProblem = spec.SpecProblem
	ProblemKind = spec.ProblemKind
)

const (
	KindString     = spec.KindString
	KindInt        = spec.KindInt
	KindFloat      = spec.KindFloat
	KindBool       = spec.KindBool
	KindDuration   = spec.KindDuration
	KindStringList = spec.KindStringList
	KindBlock      = spec.KindBlock
	KindMap        = spec.KindMap
	KindBlockList  = spec.KindBlockList

	ProblemMissing = spec.ProblemMissing
	ProblemUnknown = spec.ProblemUnknown
	ProblemShape   = spec.ProblemShape
)

// RegisterProbeSpec declares a probe's parameter schema; see spec.Register.
func RegisterProbeSpec(s ProbeSpec) { spec.Register(s) }

// ProbeSpecFor returns the schema of a probe type, if one is declared.
func ProbeSpecFor(probeType string) (ProbeSpec, bool) { return spec.For(probeType) }

// RegisteredProbeSpecs returns every declared schema, sorted by type.
func RegisteredProbeSpecs() []ProbeSpec { return spec.Registered() }

// GovernanceFields is the schema of the governance block every probe
// instance accepts; snmp_poll reuses it inside its discovery rules.
func GovernanceFields() []ParamSpec { return spec.GovernanceFields() }

// CheckGovernance reports the governance keys that are unknown or of the
// wrong shape.
func CheckGovernance(v interface{}) []SpecProblem { return spec.CheckGovernance(v) }
