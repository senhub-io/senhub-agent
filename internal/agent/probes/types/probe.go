// Package types defines core interfaces and types for the probe system
package types

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/types/datapoint"
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

	// EntitySource returns the entity.Source this probe registers with the detector.
	// Every probe MUST return a non-nil Source — the invariant test enforces this.
	// The Source describes what this probe monitors (a db.redis instance, a web.server,
	// etc.) so the entity detector can emit it into Toise topology. Host-level probes
	// and log conduits inherit the default NoOpEntitySource from BaseProbe, which
	// satisfies the invariant without polluting the entity graph.
	EntitySource() entity.Source
}

// ProbeWithCallback extends Probe for event-driven collection
type ProbeWithCallback interface {
	Probe
	// SetCallback registers handler for collected datapoints
	SetCallback(func([]datapoint.DataPoint) error)
}
