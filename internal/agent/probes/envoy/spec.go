package envoy

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "envoy", DisplayName: "Envoy Proxy", Category: "web",
		Summary:  "Server, listener and upstream cluster statistics from the Envoy admin interface; one instance per proxy.",
		DocsPath: "docs/user-guide/docs/probes/envoy.md", MultiInstance: true, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:9901", Essential: true, Group: "connection", Description: "Base URL of the admin interface"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this proxy; set it when two envoy probes run on one host"},
		},
	})
}
