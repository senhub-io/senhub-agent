//go:build !windows

// Package eventlog provides a stub for the Windows Event Log API for non-Windows platforms
package eventlog

import (
	"context"
	"fmt"
	"time"
)

// EventLevel represents the severity level of a Windows event
type EventLevel uint8

// Standard Windows event levels
const (
	EventLevelLogAlways  EventLevel = 0
	EventLevelCritical   EventLevel = 1
	EventLevelError      EventLevel = 2
	EventLevelWarning    EventLevel = 3
	EventLevelInformation EventLevel = 4
	EventLevelVerbose    EventLevel = 5
)

// Event represents a Windows event log entry
type Event struct {
	ProviderName       string
	ProviderGUID       string
	EventID            uint32
	Version            uint16
	Level              EventLevel
	TimeCreated        time.Time
	EventRecordID      uint64
	Channel            string
	Computer           string
	Message            string
	Data               map[string]string
}

// EventBatch represents a collection of events
type EventBatch struct {
	Channel  string
	Events   []Event
	Position uint64
}

// Checkpoint represents a resumption point in the event log
type Checkpoint struct {
	Channel      string
	Position     uint64
	Timestamp    time.Time
}

// ManagerOption allows configuring the Manager
type ManagerOption func(*Manager)

// WithDebug enables debug mode
func WithDebug(debug bool) ManagerOption {
	return func(m *Manager) {}
}

// WithMaxEvents sets the maximum events
func WithMaxEvents(maxEvents int) ManagerOption {
	return func(m *Manager) {}
}

// WithIncludeXML includes raw XML
func WithIncludeXML(include bool) ManagerOption {
	return func(m *Manager) {}
}

// Manager manages event collection for non-Windows platforms
type Manager struct {
	channels []string
}

// NewManager creates a new manager for non-Windows platforms
func NewManager(channels []string, options ...ManagerOption) (*Manager, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("at least one channel must be specified")
	}
	return &Manager{channels: channels}, nil
}

// Init initializes the manager
func (m *Manager) Init() error {
	return nil
}

// GetCurrentAPI returns the API name
func (m *Manager) GetCurrentAPI() string {
	return "non-windows stub"
}

// ReadEvents reads events
func (m *Manager) ReadEvents(ctx context.Context, channel string, maxEvents int) (*EventBatch, error) {
	return &EventBatch{
		Channel: channel,
		Events:  []Event{},
	}, nil
}

// Start starts event collection
func (m *Manager) Start(ctx context.Context) (<-chan *EventBatch, <-chan error) {
	eventChan := make(chan *EventBatch)
	errChan := make(chan error)
	go func() {
		defer close(eventChan)
		defer close(errChan)
		<-ctx.Done()
	}()
	return eventChan, errChan
}

// SaveCheckpoints saves checkpoints
func (m *Manager) SaveCheckpoints() error {
	return nil
}

// Close closes the manager
func (m *Manager) Close() error {
	return nil
}

// GetEventLevelName converts a level to a string
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