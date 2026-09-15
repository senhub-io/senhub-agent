package varnish

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "varnish", DisplayName: "Varnish Cache", Category: "web",
		Summary:  "Cache hits, sessions, threads and backend failures of the local Varnish through varnishstat.",
		DocsPath: "docs/user-guide/docs/probes/varnish.md", MultiInstance: false, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "varnishstat_path", Kind: probes.KindString, Default: "varnishstat", Group: "connection", Description: "Path of the varnishstat binary when it is not on the PATH", Example: "/usr/bin/varnishstat"},
			{Key: "instance_name", Kind: probes.KindString, Group: "identity", Description: "Varnish instance name passed as -n; needed when several instances run on the host"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
		},
	})
}
