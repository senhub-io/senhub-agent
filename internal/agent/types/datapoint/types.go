// Package datapoint defines data collection types
package datapoint

import (
	"senhub-agent.go/internal/agent/tags"
	"time"
)

// DataPoint represents a single data measurement point
type DataPoint struct {
	// Name identifies the data point type/source
	Name string `json:"name"`
	// Timestamp when measurement was taken
	Timestamp time.Time `json:"timestamp"`
	// Value of the measurement
	Value float32 `json:"value"`
	// Optional metadata tags
	Tags []tags.Tag `json:"tags,omitempty"`
}
