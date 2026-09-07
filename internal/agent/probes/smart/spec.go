package smart

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "smart", DisplayName: "S.M.A.R.T. Disk Health", Category: "host",
		Summary:  "Disk health attributes through smartctl.",
		DocsPath: "docs/user-guide/docs/probes/smart.md", MultiInstance: false, DefaultInterval: 300,
		Params: []probes.ParamSpec{
			{Key: "devices", Kind: probes.KindStringList, Group: "filter", Description: "Device paths to poll; empty means smartctl --scan", Example: "/dev/sda, /dev/nvme0"},
			{Key: "exclude_devices", Kind: probes.KindStringList, Group: "filter", Description: "Device paths to skip from the scan, matched exactly"},
			{Key: "smartctl_path", Kind: probes.KindString, Default: "smartctl", Group: "connection", Description: "Path of the smartctl binary when it is not on PATH"},
			{Key: "use_sudo", Kind: probes.KindBool, Default: false, Group: "connection", Description: "Prefix every smartctl call with sudo (Unix)"},
			{Key: "interval", Kind: probes.KindInt, Default: 300, Group: "collection", Description: "Seconds between collections"},
			{Key: "exec_timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Seconds one smartctl call may take"},
		},
	})
}
