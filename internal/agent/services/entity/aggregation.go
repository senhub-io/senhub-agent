package entity

import "sync/atomic"

// RelAggregatedInto links a LAG member port to its aggregate interface
// (physical → logical). Not yet in the emit SDK: the consumer registers it
// with the next pkg/emit tag, and emission stays behind
// entities.aggregation.enabled until then.
const RelAggregatedInto = "aggregated_into"

// Membership attributes, carried by the interfaces themselves: a port is a
// member of at most one aggregate, so the state is unambiguous on the port.
const (
	AttrAggregationState = "senhub.interface.aggregation.state"
	AttrAggregationMode  = "senhub.interface.aggregation.mode"
)

var aggregationEnabled atomic.Bool

// SetAggregationEnabled is set once from configuration at start.
func SetAggregationEnabled(v bool) { aggregationEnabled.Store(v) }

// AggregationEnabled reports whether aggregated_into and the membership
// attributes are emitted.
func AggregationEnabled() bool { return aggregationEnabled.Load() }
