package webapp

import "senhub-agent.go/probesdk/probes"

func init() {
	probes.RegisterProbe("load_webapp", NewLoadWebAppProbe)
	probes.RegisterProbe("ping_webapp", NewPingWebAppProbe)
}
