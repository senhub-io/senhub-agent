package event

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("event", NewEventProbe) }
