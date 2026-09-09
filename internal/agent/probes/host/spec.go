package host

import "senhub-agent.go/internal/agent/probes"

func init() {
	probes.RegisterProbeSpec(probes.ProbeSpec{
		Type:            "wifi_signal_strength",
		DisplayName:     "WiFi Signal Strength",
		Category:        "host",
		Summary:         "Signal strength and link quality of the host's WiFi connection, tagged with SSID and access point; starts only when WiFi is connected (Linux and Windows).",
		DocsPath:        "docs/user-guide/docs/probes/wifi-signal-strength.md",
		MultiInstance:   false,
		Platforms:       []string{"linux", "windows"},
		DefaultInterval: 60,
		Params:          []probes.ParamSpec{},
	})
}
