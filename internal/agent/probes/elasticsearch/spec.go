package elasticsearch

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "elasticsearch", DisplayName: "Elasticsearch", Category: "database",
		Summary:  "Cluster health, nodes and shards, plus JVM heap, indexing, search and thread pools of the local node of an Elasticsearch cluster; one instance per node.",
		DocsPath: "docs/user-guide/docs/probes/elasticsearch.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:9200", Essential: true, Group: "connection", Description: "Base URL of the node; https:// for a secured cluster", Example: "https://es01:9200"},
			{Key: "username", Kind: probes.KindString, Essential: true, Group: "auth", Description: "Basic-auth user; empty when security is disabled"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "Basic-auth password"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this node"},
		},
	})
}
