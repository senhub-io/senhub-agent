package nvidia

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "nvidia", DisplayName: "NVIDIA GPU", Category: "host",
		Summary:  "Utilization, memory, temperature, power and fan of the host's NVIDIA GPUs through nvidia-smi.",
		DocsPath: "docs/user-guide/docs/probes/nvidia.md", MultiInstance: false, DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "nvidia_smi_path", Kind: probes.KindString, Default: "nvidia-smi", Group: "connection", Description: "Path of the nvidia-smi binary when it is not on the PATH", Example: "/usr/bin/nvidia-smi"},
			{Key: "gpus", Kind: probes.KindStringList, Group: "filter", Description: "GPU indices to report, as nvidia-smi numbers them; empty means all", Example: "0, 1"},
			{Key: "interval", Kind: probes.KindInt, Default: 30, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
