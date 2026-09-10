package event

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "event",
		DisplayName:   "Event Receiver",
		Category:      "integration",
		Summary:       "HTTP endpoint (POST /event) that accepts JSON events from applications and scripts and forwards them as events; one instance per listener.",
		DocsPath:      "docs/user-guide/docs/probes/event.md",
		MultiInstance: true,
		Params: []probes.ParamSpec{
			{Key: "address", Kind: probes.KindString, Default: "127.0.0.1", Essential: true, Group: "connection", Description: "Interface address to listen on; loopback when empty, so remote senders need 0.0.0.0 or an interface address", Example: "0.0.0.0"},
			{Key: "port", Kind: probes.KindInt, Default: 5656, Essential: true, Group: "connection", Description: "HTTP port to listen on"},
			{Key: "protocol", Kind: probes.KindString, Default: "tcp", Enum: []string{"tcp"}, Group: "connection", Description: "Transport of the listener. The listener serves HTTP, so tcp is the only transport it can take"},
		},
	})
}
