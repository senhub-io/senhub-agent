package osupdates

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "os_updates",
		DisplayName:     "OS Updates",
		Category:        "host",
		Summary:         "Pending OS updates, pending security updates and reboot-required flag of the host (apt, dnf/yum, Windows Update).",
		DocsPath:        "docs/user-guide/docs/probes/os-updates.md",
		MultiInstance:   false,
		Platforms:       []string{"linux", "windows"},
		DefaultInterval: 3600,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 3600, Group: "collection", Description: "Seconds between collections; update status changes slowly"},
			{Key: "command_timeout", Kind: probes.KindInt, Default: 120, Group: "collection", Description: "Seconds allowed to the package-manager queries on Linux"},
		},
	})
}
