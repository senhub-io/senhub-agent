package probes

import (
	"senhub-agent.go/internal/agent/probes/gateway"
	"senhub-agent.go/internal/agent/probes/host"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/probes/webapp"
	"senhub-agent.go/internal/agent/services/logger"
)

type ProbeConstructor func(map[string]interface{}, *logger.Logger) (types.Probe, error)

var probeConstructors = map[string]ProbeConstructor{
	"load_webapp":          webapp.NewLoadWebAppProbe,
	"ping_webapp":          webapp.NewPingWebAppProbe,
	"ping_gateway":         gateway.NewPingGatewayProbe,
	"wifi_signal_strength": host.NewWifiSignalStrengthProbe,
	"memory":               host.NewMemoryProbe,
	"cpu":                  host.NewCpuProbe,
	"network":              host.NewNetworkProbe,
	"logicaldisk":          host.NewLogicalDiskProbe,
}
