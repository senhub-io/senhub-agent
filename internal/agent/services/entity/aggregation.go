package entity

import (
	"sync/atomic"

	"github.com/toise-dev/toise/pkg/emit/wire"
)

// RelAggregatedInto links a LAG member port to its aggregate interface
// (physical → logical). Emission stays behind entities.aggregation.enabled
// until a Toise that registers the relation is deployed.
const RelAggregatedInto = wire.RelTypeAggregatedInto

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
