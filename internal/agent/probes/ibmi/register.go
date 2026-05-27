package ibmi

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("ibmi", NewIBMiProbe) }
