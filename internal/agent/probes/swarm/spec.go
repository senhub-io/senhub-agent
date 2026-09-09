package swarm

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "swarm", DisplayName: "Docker Swarm", Category: "containers",
		Summary:  "Nodes, services, tasks and overlay networks of the swarm this manager node belongs to, through the local Docker Engine.",
		DocsPath: "docs/user-guide/docs/probes/swarm.md", MultiInstance: false, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "socket_path", Kind: probes.KindString, Group: "connection", Description: "Engine socket; /var/run/docker.sock on Unix, npipe://./pipe/docker_engine on Windows by default"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Engine request timeout in seconds"},
		},
	})
}
