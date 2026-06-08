// Package data_store is the public mirror of the small slice of the
// agent data store that probe code references directly
// (senhub-agent.go/internal/agent/services/data_store). The bulk of the
// data store (buffer, otelmapper, transformers, strategies) stays
// internal — probes only need the datapoint type, the strategy router
// interface, and the PRTG metric-id tag helper.
package data_store

import (
	ids "senhub-agent.go/internal/agent/services/data_store"
	itags "senhub-agent.go/internal/agent/tags"
)

type (
	// DataPoint is an alias of datapoint.DataPoint, kept here because
	// most probe code historically refers to it through the data_store
	// qualifier.
	DataPoint = ids.DataPoint
	// StrategyRouter is the sink-routing interface a probe may consult
	// to learn which strategies are active.
	StrategyRouter = ids.StrategyRouter
)

// CreatePrtgMetricIdTag builds the discriminant tag PRTG uses to key a
// metric channel.
func CreatePrtgMetricIdTag(metricID string) itags.Tag {
	return ids.CreatePrtgMetricIdTag(metricID)
}
