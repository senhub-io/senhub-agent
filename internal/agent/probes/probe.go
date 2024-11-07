package probes

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
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

// AllProbes is a list of all probes available
var AllProbes = []func(configuration.RemoteConfiguration) Probe{
	NewMemoryProbe,
	NewWifiSignalStrengthProbe,
}
