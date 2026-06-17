package process

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("process", NewProcessProbe) }
