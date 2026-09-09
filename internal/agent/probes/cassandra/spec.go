package cassandra

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "cassandra", DisplayName: "Apache Cassandra", Category: "database",
		Summary:  "Client connections, request counts and latencies, pending compactions, storage load and JVM heap of a Cassandra node through Jolokia; one instance per node.",
		DocsPath: "docs/user-guide/docs/probes/cassandra.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "jolokia_url", Kind: probes.KindString, Default: "http://localhost:8778/jolokia", Essential: true, Group: "connection", Description: "URL of the Jolokia agent attached to the Cassandra JVM", Example: "http://cassandra01:8778/jolokia"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this node"},
		},
	})
}
