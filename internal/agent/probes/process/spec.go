package process

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "process",
		DisplayName:     "Process Monitor",
		Category:        "host",
		Summary:         "The per-name roll-up of the local processes, and their per-process CPU, memory, threads, file descriptors and uptime once a filter names what to watch.",
		DocsPath:        "docs/user-guide/docs/probes/process.md",
		MultiInstance:   false,
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
			{Key: "filter", Kind: probes.KindBlock, Group: "filter", Description: "Narrows the process table; by_name and by_user turn on the per-process detail unless top_n is also set; empty reports the per-name roll-up over every process", Fields: []probes.ParamSpec{
				{Key: "by_name", Kind: probes.KindString, Description: "Regular expression (RE2) a process name must match", Example: "^(nginx|php-fpm)"},
				{Key: "by_user", Kind: probes.KindString, Description: "OS user owning the processes; empty accepts every user", Example: "www-data"},
				{Key: "top_n", Kind: probes.KindInt, Default: 0, Description: "Keep only the N processes ranking highest on top_by; 0 keeps all. Ranks process names, summing the measure over processes sharing a name, and reports the per-name roll-up of the N highest; per-process series need detail.per_pid"},
				{Key: "top_by", Kind: probes.KindString, Default: "cpu", Description: "What top_n ranks on: cpu, or memory (resident set, the working set on Windows)", Example: "memory"},
			}},
			{Key: "detail", Kind: probes.KindBlock, Group: "detail", Description: "Per-process series under top_n", Fields: []probes.ParamSpec{
				{Key: "per_pid", Kind: probes.KindBool, Default: false, Description: "Under top_n, emit per-process series tagged with the process id and creation time. The series set grows with every process that enters the top: only for short observations"},
			}},
			{Key: "aggregate", Kind: probes.KindBlock, Group: "detail", Description: "Roll-up of the processes sharing a name", Fields: []probes.ParamSpec{
				{Key: "enabled", Kind: probes.KindBool, Default: true, Description: "Emit one process count per distinct process name"},
			}},
		},
	})
}
