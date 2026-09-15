package memcached

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "memcached", DisplayName: "Memcached", Category: "database",
		Summary:  "Connections, items, memory, hits, misses and evictions of a Memcached server through the stats command; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/memcached.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Default: "localhost", Essential: true, Group: "connection", Description: "Server hostname or address"},
			{Key: "port", Kind: probes.KindInt, Default: 11211, Essential: true, Group: "connection", Description: "Server TCP port"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Connection and command timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server instead of host:port"},
		},
	})
}
