package redfish

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("redfish", NewRedfishProbe) }
