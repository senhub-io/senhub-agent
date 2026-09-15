package apache

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "apache", DisplayName: "Apache HTTP Server", Category: "web",
		Summary:  "Workers, connections, requests and traffic from the mod_status page; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/apache.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost/server-status?auto", Essential: true, Group: "connection", Description: "URL of the mod_status page, with ?auto"},
			{Key: "username", Kind: probes.KindString, Group: "auth", Description: "Basic-auth user when the status page is protected; empty for none"},
			{Key: "password", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Basic-auth password"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server; set it when two apache probes run on one host"},
		},
	})
}
