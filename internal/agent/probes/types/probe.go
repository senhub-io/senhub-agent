// Package types defines core interfaces and types for the probe system
package types

import (
	"context"
	"senhub-agent.go/internal/agent/types/datapoint"
	"time"
)

// Probe defines the interface that all probes must implement.
// It provides methods for lifecycle management and data collection.
type Probe interface {
	// GetName returns the unique identifier of the probe
	GetName() string

	// ShouldStart indicates if probe should be activated based on environment
	ShouldStart() bool

	// GetInterval returns the collection frequency for the probe
	GetInterval() time.Duration

	// Collect gathers metrics and returns collected datapoints
	Collect() ([]datapoint.DataPoint, error)

	// OnStart is called when probe is initialized
	// quitChannel signals when probe should stop
	OnStart(quitChannel chan struct{}) error

	// OnShutdown handles cleanup when probe is stopped
	OnShutdown(ctx context.Context) error
}

// ProbeWithCallback extends Probe for event-driven collection
type ProbeWithCallback interface {
	Probe
	// SetCallback registers handler for collected datapoints
	SetCallback(func([]datapoint.DataPoint) error)
}
