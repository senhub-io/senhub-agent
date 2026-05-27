package network

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("network", NewNetworkProbe) }
