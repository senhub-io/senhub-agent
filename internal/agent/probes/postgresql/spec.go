package postgresql

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "postgresql", DisplayName: "PostgreSQL", Category: "database",
		Summary:  "Connections, activity, replication, bloat and storage of a PostgreSQL cluster; one instance per cluster.",
		DocsPath: "docs/user-guide/docs/probes/postgresql.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Required: true, Group: "connection", Description: "Server hostname or address"},
			{Key: "port", Kind: probes.KindInt, Default: 5432, Group: "connection", Description: "Server port"},
			{Key: "username", Kind: probes.KindString, Required: true, Group: "auth", Description: "Monitoring role"},
			{Key: "password", Kind: probes.KindString, Required: true, Secret: true, Group: "auth", Description: "Role's password"},
			{Key: "database", Kind: probes.KindString, Default: "postgres", AlsoAccepts: []string{"databases"}, Group: "connection", Description: "Database the connection opens on"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindDuration, Default: "10s", Group: "collection", Description: "Query timeout, seconds or a duration"},
			{Key: "max_replication_lag_seconds", Kind: probes.KindDuration, Default: "300s", Group: "replication", Description: "Replay lag past which a replica counts as unhealthy; 0 turns the lag term off"},
			{Key: "sslmode", Kind: probes.KindString, Enum: []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}, Group: "tls", Description: "libpq SSL mode; prefer by default"},
			{Key: "sslrootcert", Kind: probes.KindString, Group: "tls", Description: "CA certificate path, libpq's name for tls.ca_file"},
			{Key: "tls", Kind: probes.KindBlock, Group: "tls", Description: "TLS settings; the block alone selects verify-full", Fields: []probes.ParamSpec{
				{Key: "skip_verify", Kind: probes.KindBool, Default: false, AlsoAccepts: []string{"insecure_skip_verify"}, Description: "Accept the server certificate without verifying it (selects require)"},
				{Key: "ca_file", Kind: probes.KindString, AlsoAccepts: []string{"ca_cert"}, Description: "CA certificate the server is verified against"},
			}},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this cluster"},
		},
	})
}
