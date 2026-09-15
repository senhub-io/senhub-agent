package dnslatency

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "dns_latency", DisplayName: "DNS Latency", Category: "network",
		Summary:  "Resolution success, time and answer count of names against one or more resolvers; one instance per name set.",
		DocsPath: "docs/user-guide/docs/probes/dns-latency.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "names", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "Names to resolve", Example: "intranet.corp.lan, www.example.com"},
			{Key: "resolvers", Kind: probes.KindStringList, Group: "targets", Description: "DNS servers as ip or ip:port, each name measured against each; empty uses the system resolver", Example: "10.0.0.53, 1.1.1.1"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Budget in seconds per lookup"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between cycles"},
		},
	})
}
