package clickhouse

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "clickhouse", DisplayName: "ClickHouse", Category: "database",
		Summary:  "Active queries, connections, memory, parts, merges and query and insert counters of a ClickHouse server from its /metrics endpoint; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/clickhouse.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8123", Essential: true, Group: "connection", Description: "Base URL of the HTTP interface", Example: "http://clickhouse01:8123"},
			{Key: "username", Kind: probes.KindString, Default: "default", Essential: true, Group: "auth", Description: "User with SELECT on the system tables"},
			{Key: "password", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "User's password; empty for a password-less user"},
			{Key: "database", Kind: probes.KindString, Default: "system", Group: "connection", Description: "Accepted and stored but unused: the probe scrapes /metrics, which is not scoped to a database"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this server"},
		},
	})
}
