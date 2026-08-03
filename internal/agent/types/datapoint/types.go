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
	Value float64 `json:"value"`
	// Optional metadata tags
	Tags []tags.Tag `json:"tags,omitempty"`
	// Histogram carries the full distribution for a native explicit-bucket
	// histogram point (OTLP-ingested). Nil for every scalar metric — the
	// overwhelmingly common case. When set, Value holds the observation
	// count so sinks without histogram rendering (PRTG, Nagios, cloud)
	// still see a meaningful numeric.
	Histogram *HistogramValue `json:"histogram,omitempty"`
}

// HistogramValue is the transport-neutral payload of one explicit-bucket
// histogram data point, mirroring the OTLP HistogramDataPoint shape.
// BucketCounts are per-bucket (non-cumulative), len(BucketCounts) ==
// len(ExplicitBounds)+1 per the OTLP spec; Sum/Min/Max are optional.
type HistogramValue struct {
	Count          uint64    `json:"count"`
	Sum            *float64  `json:"sum,omitempty"`
	Min            *float64  `json:"min,omitempty"`
	Max            *float64  `json:"max,omitempty"`
	BucketCounts   []uint64  `json:"bucket_counts,omitempty"`
	ExplicitBounds []float64 `json:"explicit_bounds,omitempty"`
}
