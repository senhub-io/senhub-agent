// senhub-agent/internal/agent/services/data_store/types.go
package data_store

import (
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
)

// DataPoint is an alias for datapoint.DataPoint
type DataPoint = datapoint.DataPoint

// StrategyRouter defines the interface for routing data to strategies
type StrategyRouter interface {
	GetTargetStrategies() []string
}

// ProbeCadenceSink is an output that wants to know how often a probe
// collects, to tell a value its probe still stands behind from a
// measurement of the past.
type ProbeCadenceSink interface {
	NoteProbeCadence(probeName string, interval time.Duration)
}

// ProbeRetireSink is an output that keeps the last values of each probe
// and must drop them once the probe no longer runs: otherwise a probe
// whose configuration was removed stays listed, with its last values,
// until those age out of the live window.
type ProbeRetireSink interface {
	ForgetProbes(probeNames []string)
}

// probeCadence is what a periodic probe exposes about its own rhythm.
type probeCadence interface {
	GetName() string
	GetInterval() time.Duration
}
