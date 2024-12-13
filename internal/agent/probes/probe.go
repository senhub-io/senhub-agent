package probes

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

// Interface each probe should implement
type Probe interface {
	// GetName returns the name of the probe
	GetName() string
	// ShouldStart returns whether the probe should be started or not
	ShouldStart() bool
	// Interval at which the probe should be run
	GetInterval() time.Duration
	// Collect runs the probe and returns the data
	Collect() ([]data_store.DataPoint, error)
	// Event called when the probe is started
	OnStart(chan struct{}) error
	// Event called when the probe is shutdown
	OnShutdown(context.Context) error
}

type ProbeConstructor func(config map[string]interface{}, logger *logger.Logger) (Probe, error)

// AllProbeDefinitions is a list of all probes available
// The key is the name of the probe in remote configuration
var probeConstructors = map[string]ProbeConstructor{
	"load_webapp":          NewLoadWebAppProbe,
	"ping_webapp":          NewPingWebAppProbe,
	"ping_gateway":         NewPingGatewayProbe,
	"wifi_signal_strength": NewWifiSignalStrengthProbe,
	"memory":               NewMemoryProbe,
	"cpu":                  NewCpuProbe,
	"network":              NewNetworkProbe,
	"storage":              NewStorageProbe,
}
