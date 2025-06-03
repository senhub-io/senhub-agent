// Package configuration provides configuration and event management for the agent
package configuration

import (
	"sync"

	"senhub-agent.go/internal/agent/services/logger"
)

// EventNotifier implements the observer pattern for configuration changes.
// It allows components to register for and receive configuration update events
// in a thread-safe manner.
type EventNotifier struct {
	observers []func(event string) // Registered callback functions
	mu        sync.Mutex           // Protects concurrent access to observers
	logger    *logger.Logger
}

// NewEventNotifier creates and initializes a new event notification system.
// Returns a pointer to EventNotifier ready to accept observers.
func NewEventNotifier(
	logger *logger.Logger,
) *EventNotifier {
	logger.Debug().Msg("Creating new EventNotifier")
	return &EventNotifier{
		logger: logger,
	}
}

// RegisterObserver adds a new callback function to be notified of events.
// The callback function accepts an event string parameter describing the change.
// Thread-safe: can be called concurrently from multiple goroutines.
func (en *EventNotifier) RegisterObserver(callback func(string)) {
	en.logger.Debug().Msg("Registering new observer")
	en.mu.Lock()
	defer en.mu.Unlock()
	en.observers = append(en.observers, callback)
}

// NotifyObservers triggers all registered callbacks with the provided event.
// Callbacks are executed asynchronously in separate goroutines.
// Thread-safe: creates a copy of observers list to prevent race conditions.
func (en *EventNotifier) NotifyObservers(event string) {
	en.logger.Info().
		Any("event", event).
		Int("observer_count", len(en.observers)).
		Msg("Notifying observers of event")

	// Copy callbacks under lock to prevent race conditions
	en.mu.Lock()
	callbacks := make([]func(string), len(en.observers))
	copy(callbacks, en.observers)
	en.mu.Unlock()

	// Execute callbacks asynchronously after releasing lock
	for _, callback := range callbacks {
		go callback(event)
	}
}
