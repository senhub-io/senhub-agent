package tomcat

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "tomcat", DisplayName: "Apache Tomcat", Category: "web",
		Summary:  "Sessions, requests, thread pools, JVM memory and GC of a Tomcat instance through Jolokia; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/tomcat.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "jolokia_url", Kind: probes.KindString, Default: "http://localhost:8080/jolokia", Essential: true, Group: "connection", Description: "URL of the Jolokia agent deployed in Tomcat", Example: "http://tomcat.example.com:8080/jolokia"},
			{Key: "username", Kind: probes.KindString, Group: "auth", Description: "Basic-auth user; empty sends no credentials"},
			{Key: "password", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Basic-auth password"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server instead of the one derived from the URL"},
		},
	})
}
