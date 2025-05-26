// Package types provides base implementations for probe interfaces
package types

import (
	"senhub-agent.go/internal/agent/services/data_store"
)

// BaseProbe provides common probe functionality that can be embedded
// in concrete probe implementations. It handles data routing and
// callback management.
type BaseProbe struct {
	OnDataPoints data_store.AddCallback // Callback for collected datapoints
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
