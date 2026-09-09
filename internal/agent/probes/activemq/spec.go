package activemq

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "activemq", DisplayName: "Apache ActiveMQ", Category: "messaging",
		Summary:  "Producers, consumers, enqueued messages, memory and store usage of an ActiveMQ broker through Jolokia; one instance per broker.",
		DocsPath: "docs/user-guide/docs/probes/activemq.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "jolokia_url", Kind: probes.KindString, Default: "http://localhost:8161/api/jolokia", Essential: true, Group: "connection", Description: "Jolokia REST endpoint of the broker", Example: "http://broker.example.com:8161/api/jolokia"},
			{Key: "broker_name", Kind: probes.KindString, Default: "localhost", Group: "connection", Description: "Broker name the MBean queries are scoped to"},
			{Key: "username", Kind: probes.KindString, Default: "admin", Essential: true, Group: "auth", Description: "Basic-auth user; empty sends no credentials"},
			{Key: "password", Kind: probes.KindString, Default: "admin", Secret: true, Essential: true, Group: "auth", Description: "Basic-auth password"},
			{Key: "queue_filter", Kind: probes.KindStringList, Group: "filter", Description: "Glob patterns of the destinations to report; empty reports every queue and topic", Example: "orders.*"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this broker; set it when the broker is reachable under several names"},
		},
	})
}
