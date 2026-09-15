package unifi

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type: "unifi", DisplayName: "UniFi Controller", Category: "network",
		Summary:  "Devices, clients, access points and WAN traffic of one UniFi site through the controller API; one instance per controller site.",
		DocsPath: "docs/user-guide/docs/probes/unifi.md", MultiInstance: true, DefaultInterval: 60,
		Params: []probes.ParamSpec{
			{Key: "endpoint", Kind: probes.KindString, Default: "https://localhost:8443", Essential: true, Group: "connection", Description: "Base URL of the controller"},
			{Key: "username", Kind: probes.KindString, Required: true, Group: "auth", Description: "Controller local user"},
			{Key: "password", Kind: probes.KindString, Required: true, Secret: true, Group: "auth", Description: "Controller local user's password"},
			{Key: "site", Kind: probes.KindString, Default: "default", Group: "targets", Description: "Controller site to watch"},
			{Key: "verify_tls", Kind: probes.KindBool, Default: true, Group: "tls", Description: "Verify the controller certificate; false accepts a self-signed one"},
			{Key: "interval", Kind: probes.KindInt, Default: 60, Group: "collection", Description: "Seconds between collections"},
			{Key: "timeout", Kind: probes.KindInt, Default: 15, Group: "collection", Description: "Request timeout in seconds"},
		},
	})
}
