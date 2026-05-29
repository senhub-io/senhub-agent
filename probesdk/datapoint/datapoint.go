// Package datapoint is the public mirror of the agent's metric datapoint
// type (senhub-agent.go/internal/agent/types/datapoint).
package datapoint

import idatapoint "senhub-agent.go/internal/agent/types/datapoint"

// DataPoint is the neutral metric record probes emit; the data store and
// every sink consume it unchanged.
type DataPoint = idatapoint.DataPoint
