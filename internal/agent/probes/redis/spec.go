package redis

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "redis", DisplayName: "Redis / Valkey", Category: "database",
		Summary:  "Health, memory, clients and keyspace of a Redis or Valkey server; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/redis.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "host", Kind: probes.KindString, Default: "127.0.0.1", Group: "connection", Description: "Server hostname or address"},
			{Key: "port", Kind: probes.KindInt, Default: 6379, Group: "connection", Description: "Server port"},
			{Key: "password", Kind: probes.KindString, Secret: true, Group: "auth", Description: "AUTH password, when required"},
			{Key: "tls", Kind: probes.KindBool, Default: false, Group: "tls", Description: "Use TLS for the connection"},
			{Key: "tls_cert_file", Kind: probes.KindString, Group: "tls", Description: "Client certificate (PEM) for mutual TLS; needs tls_key_file and tls: true"},
			{Key: "tls_key_file", Kind: probes.KindString, Group: "tls", Description: "Private key (PEM) matching tls_cert_file"},
			{Key: "tls_ca_file", Kind: probes.KindString, Group: "tls", Description: "CA bundle (PEM) to verify the server; system trust store by default"},
			{Key: "timeout", Kind: probes.KindInt, Default: 5, Group: "collection", Description: "Connection and command timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity instead of host:port"},
		},
	})
}
