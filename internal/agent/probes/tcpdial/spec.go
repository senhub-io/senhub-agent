package tcpdial

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "tcp_dial", DisplayName: "TCP Dial", Category: "network",
		Summary:  "Connect success and handshake time of TCP ports; one instance per target set.",
		DocsPath: "docs/user-guide/docs/probes/tcp-dial.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "targets", Kind: probes.KindStringList, Required: true, Group: "targets", Description: "host:port pairs to dial", Example: "10.0.0.10:443, dc01.lan:389"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Connect budget in seconds per target"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between cycles"},
		},
	})
}
