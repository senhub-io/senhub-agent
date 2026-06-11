package icmpcheck

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe(ProbeType, NewICMPCheckProbe) }
