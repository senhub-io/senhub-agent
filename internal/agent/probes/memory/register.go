package memory

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("memory", NewMemoryProbe) }
