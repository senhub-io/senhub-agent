package nats

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe(ProbeType, NewNATSProbe) }
