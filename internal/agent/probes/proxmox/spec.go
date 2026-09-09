package proxmox

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "proxmox", DisplayName: "Proxmox VE", Category: "virtualization",
		Summary:  "Nodes, virtual machines, containers and storage pools of a Proxmox VE cluster through its API; one instance per cluster.",
		DocsPath: "docs/user-guide/docs/probes/proxmox.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Required: true, Group: "connection", Description: "HTTPS base URL of the cluster API", Example: "https://pve.example.com:8006"},
			{Key: "token_id", Kind: probes.KindString, Required: true, Group: "auth", Description: "API token identifier as user@realm!tokenname", Example: "monitor@pve!agent"},
			{Key: "token_secret", Kind: probes.KindString, Required: true, Secret: true, Group: "auth", Description: "API token secret"},
			{Key: "verify_tls", Kind: probes.KindBool, Default: true, Group: "tls", Description: "Verify the API certificate; false accepts a self-signed one"},
			{Key: "node", Kind: probes.KindString, Group: "filter", Description: "Only this node is collected; empty means every node of the cluster"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 15, Group: "collection", Description: "Request timeout in seconds"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Stable identity override for this cluster"},
		},
	})
}
