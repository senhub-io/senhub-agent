package syslog

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "syslog",
		DisplayName:   "Syslog Receiver",
		Category:      "logs",
		Summary:       "Receives syslog messages (RFC 3164 and RFC 5424) over UDP or TCP and emits them as events; one instance per listener.",
		DocsPath:      "docs/user-guide/docs/probes/syslog.md",
		MultiInstance: true,
		Params: []probes.ParamSpec{
			{Key: "port", Kind: probes.KindInt, Default: 514, Essential: true, Group: "connection", Description: "Port to listen on; 514 needs root or CAP_NET_BIND_SERVICE"},
			{Key: "protocol", Kind: probes.KindString, Default: "udp", Enum: []string{"udp", "tcp"}, Essential: true, Group: "connection", Description: "Transport the listener accepts"},
			{Key: "bind_address", Kind: probes.KindString, Default: "127.0.0.1", Essential: true, Group: "connection", Description: "Interface address to listen on; loopback when empty, so remote senders need 0.0.0.0 or an interface address", Example: "0.0.0.0"},
		},
	})
}
