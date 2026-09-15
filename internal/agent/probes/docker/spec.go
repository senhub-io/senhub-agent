package docker

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "docker", DisplayName: "Docker", Category: "containers",
		Summary:  "Containers of the local Docker Engine: state, CPU, memory, network, restarts.",
		DocsPath: "docs/user-guide/docs/probes/docker.md", MultiInstance: false, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "socket_path", Kind: probes.KindString, Group: "connection", Description: "Engine socket; /var/run/docker.sock on Unix, npipe://./pipe/docker_engine on Windows by default"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Engine request timeout in seconds"},
			{Key: "include", Kind: probes.KindStringList, Group: "filter", Description: "Container name patterns to keep; empty means all", Example: "web-*"},
			{Key: "exclude", Kind: probes.KindStringList, Group: "filter", Description: "Container name patterns to drop; wins over include"},
		},
	})
}
