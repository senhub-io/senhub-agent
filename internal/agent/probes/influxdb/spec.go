package influxdb

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "influxdb", DisplayName: "InfluxDB", Category: "database",
		Summary:  "Health, storage reads and writes, query and write requests, goroutines and bucket count of an InfluxDB 2.x server; one instance per server.",
		DocsPath: "docs/user-guide/docs/probes/influxdb.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8086", Essential: true, Group: "connection", Description: "Base URL of the server", Example: "http://influx01:8086"},
			{Key: "token", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "API token; empty skips the bucket count, /health and /metrics need none"},
			{Key: "org", Kind: probes.KindString, Group: "auth", Description: "Organisation the bucket listing is scoped to; empty lists every bucket the token sees"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "HTTP request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this server"},
		},
	})
}
