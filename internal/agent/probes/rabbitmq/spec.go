package rabbitmq

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "rabbitmq", DisplayName: "RabbitMQ", Category: "messaging",
		Summary:  "Messages, queues, consumers, connections and node resources of a RabbitMQ broker through the Management API; one instance per broker.",
		DocsPath: "docs/user-guide/docs/probes/rabbitmq.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:15672", Essential: true, Group: "connection", Description: "Base URL of the Management API", Example: "http://rabbit.example.com:15672"},
			{Key: "username", Kind: probes.KindString, Default: "guest", Essential: true, Group: "auth", Description: "Management user"},
			{Key: "password", Kind: probes.KindString, Default: "guest", Secret: true, Essential: true, Group: "auth", Description: "Management user's password"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this broker instead of the one derived from the endpoint"},
		},
	})
}
