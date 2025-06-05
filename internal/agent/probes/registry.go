// Package probes provides probe registration and instantiation capabilities
package probes

import (
	"senhub-agent.go/internal/agent/probes/cpu"
	"senhub-agent.go/internal/agent/probes/event" // Import the new event probe package
	"senhub-agent.go/internal/agent/probes/gateway"
	"senhub-agent.go/internal/agent/probes/host"
	"senhub-agent.go/internal/agent/probes/logicaldisk"
	"senhub-agent.go/internal/agent/probes/memory"
	"senhub-agent.go/internal/agent/probes/network"
	"senhub-agent.go/internal/agent/probes/otel"    // Import the otel probe package
	"senhub-agent.go/internal/agent/probes/redfish" // Import the redfish probe package
	"senhub-agent.go/internal/agent/probes/syslog"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/probes/webapp"
	"senhub-agent.go/internal/agent/services/logger"
)

// ProbeConstructor defines the function signature for creating new probe instances.
// It takes configuration parameters and a base logger, returns a probe instance and potential error.
// Probes are expected to create their own ModuleLogger from the base logger.
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
// - otel: Collects OpenTelemetry data
// - redfish: Monitors hardware via Redfish API
var probeConstructors = map[string]ProbeConstructor{
	"load_webapp":          webapp.NewLoadWebAppProbe,
	"ping_webapp":          webapp.NewPingWebAppProbe,
	"ping_gateway":         gateway.NewPingGatewayProbe,
	"wifi_signal_strength": host.NewWifiSignalStrengthProbe,
	"memory":               memory.NewMemoryProbe,
	"cpu":                  cpu.NewCpuProbe,
	"network":              network.NewNetworkProbe,
	"logicaldisk":          logicaldisk.NewLogicalDiskProbe,
	"syslog":               syslog.NewSyslogProbe,
	"event":                event.NewEventProbe,
	"otel":                 otel.NewOtelProbe,
	"redfish":              redfish.NewRedfishProbe,
}
