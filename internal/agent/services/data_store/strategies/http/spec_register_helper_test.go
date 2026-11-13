package http

import "senhub-agent.go/internal/agent/probes/spec"

// registerTestSpec declares a fixture schema once per process. The spec
// registry is global and panics on a duplicate, so a plain Register in a
// test dies on the second iteration of -count=N.
func registerTestSpec(p spec.Probe) {
	if _, ok := spec.For(p.Type); !ok {
		spec.Register(p)
	}
}
