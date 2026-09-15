package hyperv

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "hyperv", DisplayName: "Windows Hyper-V", Category: "virtualization",
		Summary:  "CPU, memory and state of the virtual machines of the local Hyper-V host through WMI; Windows Server only.",
		DocsPath: "docs/user-guide/docs/probes/hyperv.md", MultiInstance: false, DefaultInterval: 60, Platforms: []string{"windows"},
		Params: []probes.ParamSpec{
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
