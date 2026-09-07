package mysql

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "mysql", DisplayName: "MySQL / MariaDB", Category: "database",
		Summary:  "Connections, queries, InnoDB, replication and storage of a MySQL or MariaDB server; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/mysql.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Default: "127.0.0.1", Group: "connection", Description: "Server hostname or address"},
			{Key: "port", Kind: probes.KindInt, Default: 3306, Group: "connection", Description: "Server port"},
			{Key: "username", Kind: probes.KindString, Group: "auth", Description: "Monitoring user"},
			{Key: "password", Kind: probes.KindString, Secret: true, Group: "auth", Description: "Monitoring user's password"},
			{Key: "database", Kind: probes.KindString, Group: "connection", Description: "Database the connection opens on; optional"},
			{Key: "tls", Kind: probes.KindBlock, Group: "tls", Description: "TLS settings, or simply true", Fields: []probes.ParamSpec{
				{Key: "enabled", Kind: probes.KindBool, Default: false, Description: "Use TLS"},
				{Key: "skip_verify", Kind: probes.KindBool, Default: false, AlsoAccepts: []string{"insecure_skip_verify"}, Description: "Accept the server certificate without verifying it"},
				{Key: "ca_file", Kind: probes.KindString, AlsoAccepts: []string{"ca_cert"}, Description: "CA certificate (PEM) the server is verified against"},
			}},
			{Key: "timeout", Kind: probes.KindDuration, Default: "10s", Group: "collection", Description: "Query timeout, seconds or a duration"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "max_replication_lag_seconds", Kind: probes.KindDuration, Default: "300s", Group: "replication", Description: "Lag past which a replica counts as unhealthy; 0 turns the lag term off"},
			{Key: "per_database", Kind: probes.KindBool, Default: false, AlsoAccepts: []string{"expose_per_database"}, Group: "detail", Description: "Emit per-database metrics"},
			{Key: "per_table", Kind: probes.KindBool, Default: false, Group: "detail", Description: "Emit per-table metrics for the largest tables; needs per_database"},
			{Key: "top_n_tables", Kind: probes.KindInt, Default: 20, AlsoAccepts: []string{"expose_top_tables"}, Group: "detail", Description: "How many tables per_table covers"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this server"},
		},
	})
}
