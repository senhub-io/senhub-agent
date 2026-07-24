// Package types provides base implementations for probe interfaces
package types

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/tags"
)

// BaseProbe provides common probe functionality that can be embedded
// in concrete probe implementations.
type BaseProbe struct {
	name      string        // Unique probe name from configuration
	probeType string        // Probe type (technical identifier: cpu, redfish, citrix, etc.)
	entitySrc entity.Source // Set by SetEntitySource in the constructor
}

// GetTargetStrategies returns the default storage strategies
// for collected metrics. "otlp" is included so that an operator who
// declares an otlp storage in their configuration sees probe data
// flow there automatically, alongside senhub/prtg/http. Strategies
// that are not configured are silently skipped by the data_store
// router, so listing them here is harmless when they're absent.
func (p *BaseProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// SetName sets the unique name for this probe instance
func (p *BaseProbe) SetName(name string) {
	p.name = name
}

// GetName returns the unique name of this probe instance
func (p *BaseProbe) GetName() string {
	return p.name
}

// SetProbeType sets the technical type for this probe (cpu, redfish, citrix, etc.)
func (p *BaseProbe) SetProbeType(probeType string) {
	p.probeType = probeType
}

// GetProbeType returns the technical type of this probe
func (p *BaseProbe) GetProbeType() string {
	return p.probeType
}

// SetEntitySource stores the entity source. Remote-target probes MUST call
// this in their constructor. Host-level probes and log conduits may omit it —
// they inherit the NoOpEntitySource fallback, which satisfies the invariant
// test without emitting unnecessary entity events.
func (p *BaseProbe) SetEntitySource(src entity.Source) { p.entitySrc = src }

// EntitySource implements Probe. Returns the configured entity source, or a
// NoOpEntitySource when SetEntitySource was never called (host-level probes
// and log conduits that don't monitor a distinct remote entity).
func (p *BaseProbe) EntitySource() entity.Source {
	if p.entitySrc == nil {
		return NoOpEntitySource{}
	}
	return p.entitySrc
}

// EnrichDataPointsWithProbeName adds the probe name and probe type tags to all datapoints
func (p *BaseProbe) EnrichDataPointsWithProbeName(datapoints []data_store.DataPoint, probeName string) []data_store.DataPoint {
	enrichedDataPoints := make([]data_store.DataPoint, len(datapoints))

	for i, dp := range datapoints {
		// Copy the datapoint
		enrichedDataPoints[i] = dp

		// Add probe_name and probe_type tags
		enrichedDataPoints[i].Tags = append([]tags.Tag{}, dp.Tags...)
		enrichedDataPoints[i].Tags = append(enrichedDataPoints[i].Tags,
			tags.Tag{
				Key:   "probe_name",
				Value: probeName,
			},
			tags.Tag{
				Key:   "probe_type",
				Value: p.probeType,
			},
		)
	}

	return enrichedDataPoints
}
