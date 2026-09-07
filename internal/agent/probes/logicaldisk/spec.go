package logicaldisk

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "logicaldisk",
		DisplayName:     "Logical Disk Metrics",
		Category:        "host",
		Summary:         "Free space and I/O per volume.",
		DocsPath:        "docs/user-guide/docs/probes/logicaldisk.md",
		MultiInstance:   false,
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Collection interval in seconds"},
			{Key: "filters", Kind: probes.KindBlock, Group: "collection", Description: "Drive selection (Windows only)", Fields: []probes.ParamSpec{
				{Key: "include", Kind: probes.KindStringList, Description: "Drive patterns to include"},
				{Key: "exclude", Kind: probes.KindStringList, Default: []string{"HarddiskVolume*", "_Total"}, Description: "Drive patterns to exclude"},
			}},
		},
	})
}
