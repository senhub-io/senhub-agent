package nats

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "nats", DisplayName: "NATS", Category: "messaging",
		Summary:  "Connections, subscriptions, message and byte throughput, routes and JetStream usage of a NATS server through its monitoring API; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/nats.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8222", Essential: true, Group: "connection", Description: "Base URL of the NATS monitoring HTTP API", Example: "http://nats.example.com:8222"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server instead of the server id it reports"},
		},
	})
}
