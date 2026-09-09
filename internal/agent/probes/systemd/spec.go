package systemd

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "systemd",
		DisplayName:     "Systemd Units",
		Category:        "host",
		Summary:         "Active state, sub-state, load state and restart count of systemd units over D-Bus (Linux only).",
		DocsPath:        "docs/user-guide/docs/probes/systemd.md",
		MultiInstance:   false,
		Platforms:       []string{"linux"},
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "units", Kind: probes.KindStringList, Group: "filter", Description: "Unit names or shell globs to watch; empty watches every unit of the included types", Example: "nginx.service, ssh*.service"},
			{Key: "include_types", Kind: probes.KindStringList, Default: []string{"service", "socket", "timer", "mount"}, Group: "filter", Description: "Unit type suffixes to include", Example: "service, timer"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
