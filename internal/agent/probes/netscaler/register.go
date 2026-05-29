package netscaler

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("netscaler", NewNetscalerProbe) }
