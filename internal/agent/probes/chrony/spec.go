package chrony

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "chrony", DisplayName: "Chrony (NTP)", Category: "host",
		Summary:  "Clock synchronisation state from chronyc (Linux and macOS).",
		DocsPath: "docs/user-guide/docs/probes/chrony.md", MultiInstance: false, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "chronyc_path", Kind: probes.KindString, Default: "chronyc", Group: "connection", Description: "Path of the chronyc binary when it is not on the service's PATH", Example: "/usr/bin/chronyc"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
