package haproxy

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "haproxy", DisplayName: "HAProxy", Category: "web",
		Summary:  "Sessions, traffic, errors and server states from the HAProxy stats CSV; one instance per load balancer.",
		DocsPath: "docs/user-guide/docs/probes/haproxy.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8080/stats;csv", Essential: true, Group: "connection", Description: "URL of the stats page in CSV form"},
			{Key: "username", Kind: probes.KindString, Group: "auth", Description: "Basic-auth user when the stats page is protected; empty for none"},
			{Key: "password", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Basic-auth password"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this load balancer; set it when two haproxy probes run on one host"},
		},
	})
}
