package host

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("wifi_signal_strength", NewWifiSignalStrengthProbe) }
