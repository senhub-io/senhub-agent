package consul

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "consul", DisplayName: "HashiCorp Consul", Category: "integration",
		Summary:  "Catalog services, Serf members, Raft and DNS latency, health checks and leadership of a Consul agent through its HTTP API; one instance per agent.",
		DocsPath: "docs/user-guide/docs/probes/consul.md", MultiInstance: true, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8500", Essential: true, Group: "connection", Description: "Base URL of the Consul HTTP API", Example: "http://consul.example.com:8500"},
			{Key: "token", Kind: probes.KindString, Secret: true, Group: "auth", Description: "ACL token sent with every request; empty when ACLs are disabled"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this agent instead of the node id it reports"},
		},
	})
}
