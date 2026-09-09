package solr

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "solr", DisplayName: "Apache Solr", Category: "database",
		Summary:  "JVM heap and threads, request count, errors and latency, cache lookups and hits, and per-core documents and index size of a Solr node; one instance per node.",
		DocsPath: "docs/user-guide/docs/probes/solr.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8983", Essential: true, Group: "connection", Description: "Base URL of the node, without the /solr path", Example: "http://solr01:8983"},
			{Key: "jolokia_url", Kind: probes.KindString, Group: "advanced", Description: "Alternative spelling of endpoint kept for consistency with the JVM probes; its scheme, host and port replace endpoint, the path is dropped"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this node"},
		},
	})
}
