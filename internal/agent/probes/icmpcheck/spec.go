package icmpcheck

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "icmp_check", DisplayName: "ICMP Check", Category: "network",
		Summary:  "Reachability, loss and round-trip time of hosts by ping.",
		DocsPath: "docs/user-guide/docs/probes/icmp-check.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "targets", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "Hostnames or addresses to ping", Example: "10.0.0.1, gw.example.com"},
			{Key: "count", Kind: probes.KindInt, Default: 4, Group: "collection", Description: "Echo requests per target per cycle"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Budget in seconds for the whole round on one target"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between cycles"},
			{Key: "packet_size", Kind: probes.KindInt, Default: 56, Group: "advanced", Description: "ICMP payload size in bytes"},
			{Key: "privileged", Kind: probes.KindBool, Group: "advanced", Description: "Raw ICMP sockets (true) or datagram sockets (false); default true on Windows and as root on Linux"},
		},
	})
}
