package ntp

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "ntp", DisplayName: "NTP", Category: "network",
		Summary:  "Clock offset, round-trip delay and stratum measured against reference NTP servers; one instance per server set.",
		DocsPath: "docs/user-guide/docs/probes/ntp.md", MultiInstance: true, DefaultInterval: 300,
		Params: []probes.ParamSpec{
			{Key: "servers", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "Reference servers as host or host:port; port 123 otherwise", Example: "ntp.example.org, 10.0.0.1:123"},
			{Key: "samples", Kind: probes.KindInt, Default: 4, Group: "collection", Description: "Exchanges per server per cycle, the least delayed is kept; at most 16"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Per-exchange timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 300, Group: "collection", Description: "Seconds between cycles; every query is traffic to somebody else's server"},
		},
	})
}
