// Package types provides base implementations for probe interfaces
package types

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// BaseProbe provides common probe functionality that can be embedded
// in concrete probe implementations. It handles data routing and
// callback management.
type BaseProbe struct {
	OnDataPoints data_store.AddCallback // Callback for collected datapoints
	name         string                  // Unique probe name from configuration
	probeType    string                  // Probe type (technical identifier: cpu, redfish, citrix, etc.)
}

// GetTargetStrategies returns the default storage strategies
// for collected metrics (senhub, prtg, and http)
func (p *BaseProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}

// SetOnDataPoints registers the callback for handling collected datapoints
func (p *BaseProbe) SetOnDataPoints(callback data_store.AddCallback) {
	p.OnDataPoints = callback
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
