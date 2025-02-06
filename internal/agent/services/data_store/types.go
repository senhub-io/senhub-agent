// senhub-agent/internal/agent/services/data_store/types.go
package data_store

import (
	"senhub-agent.go/internal/agent/types/datapoint"
)

// DataPoint est un alias vers datapoint.DataPoint
type DataPoint = datapoint.DataPoint

// StrategyRouter définit l'interface pour le routage des données vers les stratégies
type StrategyRouter interface {
	GetTargetStrategies() []string
}
