package netscaler

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("netscaler", NewNetscalerProbe) }
