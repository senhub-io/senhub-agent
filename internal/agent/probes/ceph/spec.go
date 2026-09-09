package ceph

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "ceph", DisplayName: "Ceph", Category: "storage",
		Summary:  "Cluster health, capacity, OSDs, monitor quorum and pool I/O through the Ceph Manager REST API; one instance per cluster.",
		DocsPath: "docs/user-guide/docs/probes/ceph.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "https://localhost:8443", Essential: true, Group: "connection", Description: "Base URL of the Manager dashboard / REST API"},
			{Key: "username", Kind: probes.KindString, Required: true, Group: "auth", Description: "Dashboard user"},
			{Key: "password", Kind: probes.KindString, Required: true, Secret: true, Group: "auth", Description: "Dashboard user's password"},
			{Key: "verify_tls", Kind: probes.KindBool, Default: true, Group: "tls", Description: "Verify the dashboard certificate; false accepts a self-signed one"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this cluster"},
		},
	})
}
