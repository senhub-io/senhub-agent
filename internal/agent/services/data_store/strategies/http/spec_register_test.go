package http

import "senhub-agent.go/internal/agent/probes/spec"

// The spec registry is process-wide and refuses a second declaration of a
// type, so a test that declares one must tolerate being run again in the
// same process (-count, -shuffle).
func registerProbeSpecOnce(p spec.Probe) {
	if _, declared := spec.For(p.Type); !declared {
		spec.Register(p)
	}
}
