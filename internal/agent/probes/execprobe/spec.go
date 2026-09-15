package execprobe

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "exec",
		DisplayName:     "Exec Check",
		Category:        "custom",
		Summary:         "Runs a program on a schedule and reads its Nagios or JSON output; one instance per check.",
		DocsPath:        "docs/user-guide/docs/probes/exec.md",
		MultiInstance:   true,
		DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "command", Kind: probes.KindString, Required: true, Group: "command", Description: "Absolute path of the program; no shell, no PATH lookup", Example: "/usr/local/bin/check_backup"},
			{Key: "args", Kind: probes.KindStringList, Group: "command", Description: "Arguments passed verbatim; anything secret here is stored as typed"},
			{Key: "format", Kind: probes.KindString, Default: "nagios", Enum: []string{"nagios", "json"}, Group: "command", Description: "Output format"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between runs; keep it above the timeout"},
			{Key: "timeout", Kind: probes.KindInt, Default: 10, Group: "collection", Description: "Seconds before the process group is killed"},
			{Key: "workdir", Kind: probes.KindString, Group: "command", Description: "Working directory; the agent's by default"},
			{Key: "env", Kind: probes.KindMap, Group: "command", Description: "Extra environment variables; values named like credentials are sealed"},
		},
	})
}
