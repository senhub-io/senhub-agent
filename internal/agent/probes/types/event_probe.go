// Package types provides core probe interfaces and base implementations
package types

import (
	"context"
	"senhub-agent.go/internal/agent/types/event"
	"time"
)

// EventProbe extends the base Probe interface for event-based data collection.
// It provides additional methods for event processing and callback handling.
type EventProbe interface {
	Probe
	// ProcessEvent transforms raw event data into a structured EventDataPoint
	ProcessEvent(rawData interface{}) (event.EventDataPoint, error)
	// SetEventCallback registers a handler function for processed events
	SetEventCallback(func(event.EventDataPoint) error)
}

// BaseEventProbe provides a default implementation of the EventProbe interface
// that can be embedded in concrete probe implementations.
type BaseEventProbe struct {
	name     string                           // Unique identifier for the probe
	enabled  bool                             // Whether the probe should be active
	interval time.Duration                    // Collection frequency
	callback func(event.EventDataPoint) error // Event handler function
}

// NewBaseEventProbe creates a new BaseEventProbe instance with the specified parameters
func NewBaseEventProbe(name string, enabled bool, interval time.Duration) *BaseEventProbe {
	return &BaseEventProbe{
		name:     name,
		enabled:  enabled,
		interval: interval,
	}
}

// GetName returns the probe identifier
func (p *BaseEventProbe) GetName() string {
	return p.name
}

// ShouldStart indicates if the probe should be activated
func (p *BaseEventProbe) ShouldStart() bool {
	return p.enabled
}

// GetInterval returns the collection frequency
func (p *BaseEventProbe) GetInterval() time.Duration {
	return p.interval
}

// OnStart handles probe initialization
func (p *BaseEventProbe) OnStart(stop chan struct{}) error {
	return nil
}

// OnShutdown handles cleanup when probe is stopped
func (p *BaseEventProbe) OnShutdown(ctx context.Context) error {
	return nil
}

// SetEventCallback registers the event processing callback function
func (p *BaseEventProbe) SetEventCallback(callback func(event.EventDataPoint) error) {
	p.callback = callback
}

// HandleEvent processes an event through the registered callback if available
func (p *BaseEventProbe) HandleEvent(evt event.EventDataPoint) error {
	if p.callback != nil {
		return p.callback(evt)
	}
	return nil
}
