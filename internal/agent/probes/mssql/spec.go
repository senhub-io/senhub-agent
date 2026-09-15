package mssql

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "mssql", DisplayName: "Microsoft SQL Server", Category: "database",
		Summary:  "Batch and transaction rates, connections, buffer cache, locks and per-database state and I/O of a SQL Server instance; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/mssql.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Required: true, Group: "connection", Description: "Server hostname or address; use host\\Instance for a named instance", Example: "sql01.example.com"},
			{Key: "port", Kind: probes.KindInt, Default: 1433, Essential: true, Group: "connection", Description: "Server TCP port"},
			{Key: "username", Kind: probes.KindString, Essential: true, Group: "auth", Description: "SQL login; empty selects Windows integrated authentication with the agent's account"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "SQL login password; empty with an empty username selects integrated authentication"},
			{Key: "encrypt", Kind: probes.KindString, Default: "true", Enum: []string{"true", "false", "disable", "strict"}, Group: "tls", Description: "Encryption of the connection, as go-mssqldb reads it; true by default"},
			{Key: "trust_server_cert", Kind: probes.KindBool, Default: false, Group: "tls", Description: "Accept the server certificate without verifying it"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
