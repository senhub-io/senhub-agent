package logicaldisk

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("logicaldisk", NewLogicalDiskProbe) }
