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

// uptimeMetric carries the time since the host booted, in seconds. A
// restart shows as the value dropping, which is how a monitoring system
// tells a reboot from an agent restart.
const uptimeMetric = "system_uptime"

// withClock adds the host clock, and the uptime when a source is given, to
// what the platform collector returns. The clock is read when the point is
// built, not at the cycle's start: the unix collector blocks for a second
// sampling CPU usage, which would otherwise be read as drift.
type withClock struct {
	hostpoll.Collector
	now func() time.Time
	// uptime reads the seconds since boot; nil leaves the point out.
	uptime func() (uint64, error)
	// onUptimeError is told why the uptime point is missing this cycle.
	onUptimeError func(error)
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
	points = append(points, datapoint.DataPoint{
		Name:      clockMetric,
		Timestamp: timestamp,
		Value:     float64(sampled.Unix()) + float64(sampled.Nanosecond())/1e9,
		Tags:      baseTags,
	})
	if c.uptime != nil {
		seconds, uerr := c.uptime()
		if uerr != nil {
			if c.onUptimeError != nil {
				c.onUptimeError(uerr)
			}
			return points, nil
		}
		points = append(points, datapoint.DataPoint{
			Name:      uptimeMetric,
			Timestamp: timestamp,
			Value:     float64(seconds),
			Tags:      baseTags,
		})
	}
	return points, nil
}
