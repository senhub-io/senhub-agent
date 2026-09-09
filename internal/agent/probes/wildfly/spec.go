package wildfly

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "wildfly", DisplayName: "WildFly / JBoss EAP", Category: "web",
		Summary:  "JVM memory and GC, Undertow requests and threads, JTA transactions and JDBC pools of a WildFly server through the HTTP Management API; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/wildfly.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:9990", Essential: true, Group: "connection", Description: "Base URL of the management interface", Example: "http://wildfly.example.com:9990"},
			{Key: "username", Kind: probes.KindString, Default: "admin", Essential: true, Group: "auth", Description: "Management user"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "Management user's password"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this server instead of the one derived from the endpoint"},
		},
	})
}
