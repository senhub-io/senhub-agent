package zookeeper

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "zookeeper", DisplayName: "Apache ZooKeeper", Category: "messaging",
		Summary:  "Latency, connections, packets, znodes, watches and leader state of a ZooKeeper node through the mntr command; one instance per node.",
		DocsPath: "docs/user-guide/docs/probes/zookeeper.md", MultiInstance: true, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Default: "localhost", Essential: true, Group: "connection", Description: "Node hostname or address"},
			{Key: "port", Kind: probes.KindInt, Default: 2181, Essential: true, Group: "connection", Description: "Client port the four-letter commands are sent to"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Connection and command timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this node instead of host:port"},
		},
	})
}
