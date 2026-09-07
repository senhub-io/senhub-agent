package linuxlogs

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:          "linux_logs",
		DisplayName:   "Linux Journal Logs",
		Category:      "logs",
		Summary:       "Streams systemd journal entries as logs (Linux only).",
		DocsPath:      "docs/user-guide/docs/probes/linux-logs.md",
		MultiInstance: true,
		Params: []probes.ParamSpec{
			{Key: "units", Kind: probes.KindStringList, Group: "filter", Description: "systemd units to follow; empty means every unit", Example: "nginx.service"},
			{Key: "identifiers", Kind: probes.KindStringList, Group: "filter", Description: "Program names (SYSLOG_IDENTIFIER) to follow", Example: "sshd"},
			{Key: "priority", Kind: probes.KindInt, Default: 7, Group: "filter", Description: "Highest syslog priority to include, 0 (emergency) to 7 (debug)"},
			{Key: "include_boot", Kind: probes.KindBool, Default: false, Group: "collection", Description: "Replay entries since the current boot instead of streaming only new ones"},
		},
	})
}
