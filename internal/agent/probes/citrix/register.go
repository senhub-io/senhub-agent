package citrix

import "senhub-agent.go/probesdk/probes"

func init() { probes.RegisterProbe("citrix", NewCitrixProbe) }
