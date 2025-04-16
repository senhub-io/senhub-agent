//go:build windows

package eventlog

import (
	"context"
	"time"
	"golang.org/x/sys/windows"
)

// API defines the interface for interacting with Windows Event Log
type API interface {
	// Name returns the name of the API implementation
	Name() string
	
	// IsAvailable checks if the API is available on the current system
	IsAvailable() bool
	
	// ListChannels lists available event log channels
	ListChannels(ctx context.Context) ([]string, error)
	
	// Open opens a channel for reading
	Open(channel string) (windows.Handle, error)
	
	// Close closes a channel handle
	Close(handle windows.Handle) error
	
	// Read reads events from a channel
	Read(ctx context.Context, handle windows.Handle, maxEvents int, cp *Checkpoint) (*EventBatch, error)
	
	// SubscribeToEvents subscribes to events from a channel
	SubscribeToEvents(ctx context.Context, channel string, cp *Checkpoint) (<-chan *EventBatch, <-chan error)
}

// APIFactory defines a function that creates an API implementation
type APIFactory func(includeXML bool) (API, error)

// apiFactories holds registered API implementations
var apiFactories = map[string]APIFactory{}

// RegisterAPI registers an API implementation
func RegisterAPI(name string, factory APIFactory) {
	apiFactories[name] = factory
}

// GetAPI returns an API by name
func GetAPI(name string, includeXML bool) (API, error) {
	if factory, ok := apiFactories[name]; ok {
		return factory(includeXML)
	}
	return nil, nil
}

// GetAvailableAPIs returns all available API implementations
func GetAvailableAPIs(includeXML bool) []API {
	var apis []API
	for _, factory := range apiFactories {
		api, err := factory(includeXML)
		if err == nil && api != nil && api.IsAvailable() {
			apis = append(apis, api)
		}
	}
	return apis
}

// GetPreferredAPI returns the preferred API implementation
// (Modern API if available, otherwise Legacy API)
func GetPreferredAPI(includeXML bool) (API, error) {
	// Try Modern API first
	modernAPI, err := GetAPI("modern", includeXML)
	if err == nil && modernAPI != nil && modernAPI.IsAvailable() {
		return modernAPI, nil
	}
	
	// Fallback to Legacy API
	legacyAPI, err := GetAPI("legacy", includeXML)
	if err == nil && legacyAPI != nil && legacyAPI.IsAvailable() {
		return legacyAPI, nil
	}
	
	return nil, nil
}

// GetEventLevelName converts a level number to a string
func GetEventLevelName(level EventLevel) string {
	switch level {
	case EventLevelLogAlways:
		return "LogAlways"
	case EventLevelCritical:
		return "Critical"
	case EventLevelError:
		return "Error"
	case EventLevelWarning:
		return "Warning"
	case EventLevelInformation:
		return "Information"
	case EventLevelVerbose:
		return "Verbose"
	default:
		return "Unknown"
	}
}
