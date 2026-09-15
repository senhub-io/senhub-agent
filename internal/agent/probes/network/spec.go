package network

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "network",
		DisplayName:     "Network Interface",
		Category:        "host",
		Summary:         "Traffic, packets, errors and discards per interface.",
		DocsPath:        "docs/user-guide/docs/probes/network.md",
		MultiInstance:   false,
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Collection interval in seconds"},
		},
	})
}
