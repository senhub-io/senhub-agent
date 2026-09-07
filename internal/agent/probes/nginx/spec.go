package nginx

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "nginx", DisplayName: "Nginx", Category: "web",
		Summary:  "Connections and requests from the nginx stub_status page; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/nginx.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost/nginx_status", Group: "connection", Description: "URL of the stub_status page"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server; set it when two nginx probes run on one host"},
		},
	})
}
