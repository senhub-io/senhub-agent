// Package spec is the public mirror of the agent's probe parameter schema
// (senhub-agent.go/internal/agent/probes/spec). A probe declares its
// parameters once, from an init(), and the web configurator, the
// catalogue and `agent config check` all read that declaration. Probe
// packages from the separate senhub-agent-enterprise module use this
// mirror because Go forbids importing senhub-agent.go/internal/... across
// module boundaries.
package spec

import ispec "senhub-agent.go/internal/agent/probes/spec"

type (
	ParamKind = ispec.ParamKind
	ParamSpec = ispec.ParamSpec
	Condition = ispec.Condition
	Probe     = ispec.Probe
)

const (
	KindString     = ispec.KindString
	KindInt        = ispec.KindInt
	KindFloat      = ispec.KindFloat
	KindBool       = ispec.KindBool
	KindDuration   = ispec.KindDuration
	KindStringList = ispec.KindStringList
	KindBlock      = ispec.KindBlock
	KindMap        = ispec.KindMap
	KindBlockList  = ispec.KindBlockList
)

// Register declares a probe type's parameters. Call it from the probe's
// init(), next to RegisterProbe.
func Register(p Probe) { ispec.Register(p) }

// For returns the declaration of a probe type, if it has one.
func For(probeType string) (Probe, bool) { return ispec.For(probeType) }

// Registered returns every declared schema, sorted by type.
func Registered() []Probe { return ispec.Registered() }
