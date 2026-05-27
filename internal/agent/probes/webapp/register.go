package webapp

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbe("load_webapp", NewLoadWebAppProbe)
	probes.RegisterProbe("ping_webapp", NewPingWebAppProbe)
}
