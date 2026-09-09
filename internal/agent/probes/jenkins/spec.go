package jenkins

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "jenkins", DisplayName: "Jenkins", Category: "integration",
		Summary:  "Job results, last-build durations, nodes, executors and queue depth of a Jenkins controller through its REST API; one instance per controller.",
		DocsPath: "docs/user-guide/docs/probes/jenkins.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Required: true, Group: "connection", Description: "Base URL of the controller", Example: "https://jenkins.example.com"},
			{Key: "username", Kind: probes.KindString, Essential: true, Group: "auth", Description: "User the API calls authenticate as; empty queries anonymously"},
			{Key: "api_token", Kind: probes.KindString, Secret: true, Essential: true, Group: "auth", Description: "API token of that user"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 15, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity of this controller instead of the one it reports"},
		},
	})
}
