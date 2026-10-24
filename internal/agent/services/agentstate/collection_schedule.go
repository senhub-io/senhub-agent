package agentstate

import (
	"sync"
	"time"
)

// scheduleState keeps the process-wide probe scheduling counters. The lag map
// is read-and-reset from BuildAgentRecords: the agentmetrics exposition
// interval defines the collection window, so no separate timer is needed as
// long as the builder is called once per exposition cycle.
var scheduleState = struct {
	mu sync.Mutex

	inFlight             int64
	collectLag           map[string]time.Duration
	overruns             map[string]uint64
	firstCollectionDelay map[string]time.Duration
}{
	collectLag:           map[string]time.Duration{},
	overruns:             map[string]uint64{},
	firstCollectionDelay: map[string]time.Duration{},
}

// normalizeProbe replaces an empty probe type with the label used by the other
// agentstate probe-keyed metrics, so every scheduling metric shares the same
// bounded cardinality contract.
func normalizeProbe(probe string) string {
	if probe == "" {
		return "unknown"
	}
	return probe
}

// CollectionStarted increments the in-flight gauge so BuildAgentRecords can
// expose the instantaneous number of running collections.
func CollectionStarted() {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	scheduleState.inFlight++
}

// CollectionEnded decrements the in-flight gauge, flooring at zero.
func CollectionEnded() {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	if scheduleState.inFlight > 0 {
		scheduleState.inFlight--
	}
}

// CollectionsInFlight returns the current number of running collections.
func CollectionsInFlight() int64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	return scheduleState.inFlight
}

// RecordCollectLag keeps the largest non-negative lag seen since the last
// TakeCollectLagWindow. A negative lag is treated as 0: scheduling ahead of
// plan is not a delay, and the key still exists once recorded.
func RecordCollectLag(probe string, lag time.Duration) {
	probe = normalizeProbe(probe)
	if lag < 0 {
		lag = 0
	}
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	if cur, ok := scheduleState.collectLag[probe]; !ok || lag > cur {
		scheduleState.collectLag[probe] = lag
	}
}

// TakeCollectLagWindow returns the max lag per probe for the window that just
// ended and resets every stored max to zero while keeping the probe keys.
// Keeping keys matters for dashboards: a probe that missed this window must
// report 0, not disappear until its next lag sample.
func TakeCollectLagWindow() map[string]float64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()

	out := make(map[string]float64, len(scheduleState.collectLag))
	for probe, lag := range scheduleState.collectLag {
		out[probe] = lag.Seconds()
		scheduleState.collectLag[probe] = 0
	}
	return out
}

// IncrementCollectOverruns records scheduler ticks skipped because a previous
// collection of the same probe type was still running. Non-positive ticks are
// ignored so callers don't have to guard a default zero value.
func IncrementCollectOverruns(probe string, ticks int) {
	if ticks <= 0 {
		return
	}
	probe = normalizeProbe(probe)
	scheduleState.mu.Lock()
	scheduleState.overruns[probe] += uint64(ticks)
	scheduleState.mu.Unlock()
}

// GetCollectOverruns returns a snapshot copy of the cumulative overrun ticks
// per probe type.
func GetCollectOverruns() map[string]uint64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	out := make(map[string]uint64, len(scheduleState.overruns))
	for probe, n := range scheduleState.overruns {
		out[probe] = n
	}
	return out
}

// RecordFirstCollectionDelay keeps the largest first-collection duration seen
// for a probe type. Several configured instances of one type finish at
// different times; the maximum is the conservative, useful value.
func RecordFirstCollectionDelay(probe string, d time.Duration) {
	probe = normalizeProbe(probe)
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	if cur, ok := scheduleState.firstCollectionDelay[probe]; !ok || d > cur {
		scheduleState.firstCollectionDelay[probe] = d
	}
}

// GetFirstCollectionDelays returns a snapshot copy of the maximum first
// collection delay per probe type, in seconds.
func GetFirstCollectionDelays() map[string]float64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	out := make(map[string]float64, len(scheduleState.firstCollectionDelay))
	for probe, d := range scheduleState.firstCollectionDelay {
		out[probe] = d.Seconds()
	}
	return out
}

// ResetCollectionScheduleForTest clears every scheduling counter and map so a
// test counting records does not depend on which package tests ran first.
func ResetCollectionScheduleForTest() {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	scheduleState.inFlight = 0
	scheduleState.collectLag = map[string]time.Duration{}
	scheduleState.overruns = map[string]uint64{}
	scheduleState.firstCollectionDelay = map[string]time.Duration{}
}
