package winservices

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "winservices",
		DisplayName:     "Windows Services",
		Category:        "host",
		Summary:         "State of Windows services (Windows only).",
		DocsPath:        "docs/user-guide/docs/probes/windows-services.md",
		MultiInstance:   false,
		Platforms:       []string{"windows"},
		DefaultInterval: 30,
		Params: []probes.ParamSpec{
			{Key: "services", Kind: probes.KindStringList, Group: "filter", Description: "Service short names to monitor; empty means services configured for automatic start", Example: "wuauserv, Spooler"},
			{Key: "detail", Kind: probes.KindString, Default: "state", Group: "collection", Description: "Metrics detail: state emits only host.service.running per service; full adds windows.service.state, status and start type"},
			{Key: "interval", Kind: probes.KindDuration, Default: "30s", Group: "collection", Description: "Collection interval"},
		},
	})
}
