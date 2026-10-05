package cpu

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// clockMetric carries the host clock, in seconds since the Unix epoch.
const clockMetric = "system_time"

// withClock adds the host clock to what the platform collector returns.
// The value is read when the point is built, not at the cycle's start:
// the unix collector blocks for a second sampling CPU usage, which would
// otherwise be read as drift.
type withClock struct {
	hostpoll.Collector
	now func() time.Time
}

func (c withClock) Collect(timestamp time.Time) ([]datapoint.DataPoint, error) {
	points, err := c.Collector.Collect(timestamp)
	if err != nil {
		return points, err
	}
	baseTags, err := common.GetHostTags()
	if err != nil {
		return nil, fmt.Errorf("error getting host tags: %w", err)
	}
	sampled := c.now()
	return append(points, datapoint.DataPoint{
		Name:      clockMetric,
		Timestamp: timestamp,
		Value:     float64(sampled.Unix()) + float64(sampled.Nanosecond())/1e9,
		Tags:      baseTags,
	}), nil
}
