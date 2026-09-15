package memory

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "memory",
		DisplayName:     "Memory Metrics",
		Category:        "host",
		Summary:         "Physical memory and swap usage.",
		DocsPath:        "docs/user-guide/docs/probes/memory.md",
		MultiInstance:   false,
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Collection interval in seconds"},
		},
	})
}
