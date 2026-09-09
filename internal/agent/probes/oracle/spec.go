package oracle

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "oracle", DisplayName: "Oracle Database", Category: "database",
		Summary:  "Sessions, SGA and PGA memory, buffer cache, tablespaces, wait classes and deadlocks of an Oracle database; one instance per service.",
		DocsPath: "docs/user-guide/docs/probes/oracle.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Required: true, Group: "connection", Description: "Listener hostname or address", Example: "db.example.com"},
			{Key: "port", Kind: probes.KindInt, Default: 1521, Essential: true, Group: "connection", Description: "Listener port"},
			{Key: "service_name", Kind: probes.KindString, Required: true, Group: "connection", Description: "Oracle service name, not the SID", Example: "ORCL"},
			{Key: "username", Kind: probes.KindString, Required: true, Group: "auth", Description: "Database user with SELECT on the v$ views"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "User's password"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
