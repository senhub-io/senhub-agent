package pulsar

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "pulsar", DisplayName: "Apache Pulsar", Category: "messaging",
		Summary:  "Readiness, message and byte rates, storage and backlog of a Pulsar broker from its admin API and metrics endpoint; one instance per broker.",
		DocsPath: "docs/user-guide/docs/probes/pulsar.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "http://localhost:8080", Essential: true, Group: "connection", Description: "Base URL of the broker admin and metrics HTTP service", Example: "http://pulsar.example.com:8080"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this broker instead of the one derived from the endpoint"},
		},
	})
}
