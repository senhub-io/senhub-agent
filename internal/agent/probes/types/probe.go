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

	// EntitySource returns the entity.Source the ProbePoller registers with the
	// detector on Start (and unregisters on Shutdown) — the single source of
	// truth for probe entity wiring; probes never call entity.RegisterSource
	// themselves (#471). Every probe MUST return a non-nil Source — the
	// invariant test enforces this. The Source describes what this probe
	// monitors (a db.redis instance, a web.server, etc.) so the entity detector
	// can emit it into Toise topology. Host-level probes and log conduits
	// inherit the default NoOpEntitySource from BaseProbe, which satisfies the
	// invariant and is skipped at registration time.
	EntitySource() entity.Source
}

// ProbeWithCallback extends Probe for event-driven collection
type ProbeWithCallback interface {
	Probe
	// SetCallback registers handler for collected datapoints
	SetCallback(func([]datapoint.DataPoint) error)
}

// ListenerProbe is implemented by a probe whose data arrives by
// subscription — a socket, a receiver, an OS event log — rather than by
// polling a target on a schedule.
//
// The distinction exists because health means something different for
// the two. A polling probe is healthy when its last Collect succeeded;
// a listener probe's Collect has nothing to do, so "it returned no
// error" says only that the no-op ran. Seven probes reported healthy
// that way while their listener could have been dead for hours (#289).
//
// A listener probe reports the state of the thing that actually carries
// its data: the socket is bound, the receiver is serving, the
// subscription is open. ProbePoller asks this instead of inferring
// health from the collection cycle.
type ListenerProbe interface {
	Probe

	// ListenerHealth returns nil while the listener is able to receive,
	// and the reason it cannot otherwise. It must not block: it is
	// called on every collection cycle and reports state the probe
	// already holds.
	ListenerHealth() error
}
