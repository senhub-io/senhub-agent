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
			{Key: "include_virtual_interfaces", Kind: probes.KindBool, Default: false, Group: "collection", Description: "Collect container/virtualisation interfaces; false skips virtual interfaces to keep default metric series minimal, true restores every interface the probe accepts."},
			{Key: "address_label", Kind: probes.KindBool, Default: false, Group: "collection", Description: "Add the primary interface address as a per-series IP label; false keeps dynamic address changes from creating new series, true restores the previous IP tag."},
		},
	})
}
