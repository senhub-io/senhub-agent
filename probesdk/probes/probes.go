// Package probes is the public mirror of the agent's probe registry.
//
// It re-exports the registration entrypoint so probe packages — both
// the free-tier probes in this repository and the paid probes shipped
// from the separate senhub-agent-enterprise module — can self-register
// without importing senhub-agent.go/internal/..., which Go forbids
// across module boundaries. See the probesdk/ tree for the rest of the
// probe-authoring contract.
package probes

import iprobes "senhub-agent.go/internal/agent/probes"

// ProbeConstructor is the function signature a probe package exposes
// from its New<Name>Probe constructor. It is an alias of the internal
// type, so a constructor written against this package satisfies the
// registry without conversion.
type ProbeConstructor = iprobes.ProbeConstructor

// RegisterProbe wires a probe constructor under its YAML `type:` name.
// Call it from an init() in the probe's own package.
func RegisterProbe(name string, ctor ProbeConstructor) {
	iprobes.RegisterProbe(name, ctor)
}
