// Package probes provides probe registration and instantiation capabilities
package probes

import (
	"senhub-agent.go/internal/agent/probes/event" // Import the new event probe package
	"senhub-agent.go/internal/agent/probes/event/winevents" // Import the Windows events probe package
	"senhub-agent.go/internal/agent/probes/gateway"
	"senhub-agent.go/internal/agent/probes/host"
	"senhub-agent.go/internal/agent/probes/redfish" // Import the redfish probe package
	"senhub-agent.go/internal/agent/probes/systemlogs" // Import the system logs probe package
	"senhub-agent.go/internal/agent/probes/syslog"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/probes/webapp"
	"senhub-agent.go/internal/agent/services/logger"
)

// ProbeConstructor defines the function signature for creating new probe instances.
// It takes configuration parameters and a logger, returns a probe instance and potential error.
type ProbeConstructor func(map[string]interface{}, *logger.Logger) (types.Probe, error)

// probeConstructors maps probe names to their constructor functions.
// This registry allows dynamic probe creation based on configuration.
//
// Supported probes:
// - load_webapp: Measures webapp loading metrics
// - ping_webapp: Tests webapp availability
// - ping_gateway: Monitors gateway connectivity
// - wifi_signal_strength: Measures WiFi signal quality
// - memory: Tracks memory usage
// - cpu: Monitors CPU utilization
// - network: Collects network interface metrics
// - logicaldisk: Monitors disk space and IO
// - syslog: Collects system logs
// - event: Collects custom events via HTTP
// - winevents: Collects Windows Event Log entries (Deprecated: use systemlogs instead)
// - systemlogs: Collects system logs appropriate for the platform
// - redfish: Monitors hardware via Redfish API
var probeConstructors = map[string]ProbeConstructor{
	"load_webapp":          webapp.NewLoadWebAppProbe,
	"ping_webapp":          webapp.NewPingWebAppProbe,
	"ping_gateway":         gateway.NewPingGatewayProbe,
	"wifi_signal_strength": host.NewWifiSignalStrengthProbe,
	"memory":               host.NewMemoryProbe,
	"cpu":                  host.NewCpuProbe,
	"network":              host.NewNetworkProbe,
	"logicaldisk":          host.NewLogicalDiskProbe,
	"syslog":               syslog.NewSyslogProbe,
	"event":                event.NewEventProbe,
	"winevents":            winevents.NewWinEventProbe,     // Deprecated: use systemlogs instead
	"systemlogs":           systemlogs.NewSystemLogsProbe,  // Cross-platform system logs collection
	"redfish":              redfish.NewRedfishProbe,
}
