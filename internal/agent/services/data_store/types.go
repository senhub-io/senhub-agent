// senhub-agent/internal/agent/services/data_store/types.go
package data_store

import (
	"senhub-agent.go/internal/agent/types/datapoint"
)

// DataPoint is an alias for datapoint.DataPoint
type DataPoint = datapoint.DataPoint

// StrategyRouter defines the interface for routing data to strategies
type StrategyRouter interface {
	GetTargetStrategies() []string
}
