package configuration

import "sync"

type EventNotifier struct {
	observers []func(event string)
	mu        sync.Mutex
}

// NewEventNotifier initializes an EventNotifier with a callback.
func NewEventNotifier() *EventNotifier {
	return &EventNotifier{}
}

// RegisterObserver allows a service to register a callback function.
func (en *EventNotifier) RegisterObserver(callback func(string)) {
	en.mu.Lock()
	defer en.mu.Unlock()
	en.observers = append(en.observers, callback)
}

// Notify triggers the callback with the given event data.
func (en *EventNotifier) NotifyObservers(event string) {
	en.mu.Lock()
	for _, callback := range en.observers {
		// Call each observer callback with the new configuration
		go callback(event)
	}
	en.mu.Unlock()
}
