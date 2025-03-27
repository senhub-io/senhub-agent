// Package event defines event handling types and validation
package event

import (
	"fmt"
	"time"
)

// EventSeverity defines possible severity levels
type EventSeverity string

const (
	Emergency     EventSeverity = "emerg"
	Alert         EventSeverity = "alert"
	Critical      EventSeverity = "crit"
	Error         EventSeverity = "err"
	Warning       EventSeverity = "warning"
	Notice        EventSeverity = "notice"
	Informational EventSeverity = "info"
	Debug         EventSeverity = "debug"
)

// EventDataPoint is a dynamic map storing event fields
type EventDataPoint map[string]interface{}

// HasKey checks if the EventDataPoint contains a specific key
func (e EventDataPoint) HasKey(key string) bool {
	_, exists := e[key]
	return exists
}

// Validate checks required fields presence and types
func (e EventDataPoint) Validate() error {
	timestamp, ok := e["timestamp"].(time.Time)
	if !ok || timestamp.IsZero() {
		return fmt.Errorf("timestamp is required and must be a valid time")
	}

	host, ok := e["host"].(string)
	if !ok || host == "" {
		return fmt.Errorf("host is required and must be a non-empty string")
	}

	severity, ok := e["severity"].(string)
	if !ok || severity == "" {
		return fmt.Errorf("severity is required and must be a valid severity level")
	}

	message, ok := e["message"].(string)
	if !ok || message == "" {
		return fmt.Errorf("message is required and must be a non-empty string")
	}

	return nil
}

// NewEventDataPoint creates event point with required fields
func NewEventDataPoint(timestamp time.Time, host string, severity EventSeverity, message string) EventDataPoint {
	return EventDataPoint{
		"timestamp": timestamp,
		"host":      host,
		"severity":  string(severity),
		"message":   message,
	}
}
