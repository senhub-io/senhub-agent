// probes/types/probe.go
package types

import (
	"context"
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
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

// ProbeWithCallback extends Probe interface for probes that need to handle callbacks
type ProbeWithCallback interface {
	Probe
	// SetCallback allows setting a callback function for data handling
	SetCallback(func([]data_store.DataPoint) error)
}
