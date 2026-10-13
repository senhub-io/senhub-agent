package cpu

import (
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/hostpoll"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// clockMetric carries the host clock, in seconds since the Unix epoch.
const clockMetric = "system_time"

// uptimeMetric carries the time since the host booted, in seconds. A
// restart shows as the value dropping, which is how a monitoring system
// tells a reboot from an agent restart.
const uptimeMetric = "system_uptime"

// clockSync is a single reading of the kernel's clock synchronisation state.
type clockSync struct {
	synced   bool
	offset   time.Duration
	maxError time.Duration
	// maxErrorUnknown leaves system_clock_max_error out: Windows has no
	// such bound, and a 0 would read as a perfect clock.
	maxErrorUnknown bool
	source          string
}

// withClock adds the host clock, clock synchronisation state, and the uptime
// when a source is given, to what the platform collector returns. The clock
// is read when the point is built, not at the cycle's start: the unix
// collector blocks for a second sampling CPU usage, which would otherwise be
// read as drift.
type withClock struct {
	hostpoll.Collector
	now func() time.Time
	// uptime reads the seconds since boot; nil leaves the point out.
	uptime func() (uint64, error)
	// onUptimeError is told why the uptime point is missing this cycle.
	onUptimeError func(error)
	// sync reads the clock synchronisation state; nil leaves the points out.
	sync func() (clockSync, error)
	// onSyncError is told why the clock synchronisation points are missing.
	onSyncError func(error)
	// lastSyncError deduplicates repeated sync errors so the log stays quiet.
	lastSyncError string
}

func (c *withClock) Collect(timestamp time.Time) ([]datapoint.DataPoint, error) {
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

	if c.sync != nil {
		syncState, serr := c.sync()
		if serr != nil {
			if c.onSyncError != nil && c.lastSyncError != serr.Error() {
				c.onSyncError(serr)
				c.lastSyncError = serr.Error()
			}
		} else {
			c.lastSyncError = ""
			syncTags := make([]tags.Tag, len(baseTags), len(baseTags)+1)
			copy(syncTags, baseTags)
			syncTags = append(syncTags, tags.Tag{
				Key:   "clock_source",
				Value: syncState.source,
			})

			synchronizedValue := 0.0
			if syncState.synced {
				synchronizedValue = 1
			}
			points = append(points, datapoint.DataPoint{
				Name:      "system_clock_synchronized",
				Timestamp: timestamp,
				Value:     synchronizedValue,
				Tags:      syncTags,
			})
			if !syncState.maxErrorUnknown {
				points = append(points, datapoint.DataPoint{
					Name:      "system_clock_max_error",
					Timestamp: timestamp,
					Value:     syncState.maxError.Seconds(),
					Tags:      syncTags,
				})
			}
			if syncState.synced {
				points = append(points, datapoint.DataPoint{
					Name:      "system_clock_offset",
					Timestamp: timestamp,
					Value:     syncState.offset.Seconds(),
					Tags:      syncTags,
				})
			}
		}
	}

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
