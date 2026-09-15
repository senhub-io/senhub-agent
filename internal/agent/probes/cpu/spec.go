package cpu

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "cpu",
		DisplayName:     "CPU Metrics",
		Category:        "host",
		Summary:         "CPU usage, per core and by mode.",
		DocsPath:        "docs/user-guide/docs/probes/cpu.md",
		MultiInstance:   false,
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Collection interval in seconds"},
		},
	})
}
