package otlpreceiver

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("otlp_receiver", NewOTLPReceiverProbe) }
