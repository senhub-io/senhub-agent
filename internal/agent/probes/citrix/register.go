package citrix

import "senhub-agent.go/internal/agent/probes"

func init() { probes.RegisterProbe("citrix", NewCitrixProbe) }
