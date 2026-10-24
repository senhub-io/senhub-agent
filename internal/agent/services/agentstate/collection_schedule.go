package agentstate

import (
	"sync"
	"time"
)

// scheduleState keeps the process-wide probe scheduling counters. The lag
// maxima are windowed per reader: each exposition path (Prometheus, OTLP)
// reads the maximum since its own previous read, so one reader's read never
// resets what another reader is about to see.
var scheduleState = struct {
	mu sync.Mutex

	inFlight int64
	// lagSinceStart is the maximum lag per probe since the process
	// started. A reader seen for the first time starts from it, so its
	// first window is not blind to the collections that ran before it read.
	lagSinceStart        map[string]time.Duration
	lagWindows           map[string]map[string]time.Duration
	overruns             map[string]uint64
	firstCollectionDelay map[string]time.Duration
}{
	lagSinceStart:        map[string]time.Duration{},
	lagWindows:           map[string]map[string]time.Duration{},
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

// RecordCollectLag keeps the largest non-negative lag seen, for every
// reader window and since the process started. A negative lag is treated as
// 0: scheduling ahead of plan is not a delay, and the key still exists once
// recorded.
func RecordCollectLag(probe string, lag time.Duration) {
	probe = normalizeProbe(probe)
	if lag < 0 {
		lag = 0
	}
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	raiseTo(scheduleState.lagSinceStart, probe, lag)
	for _, window := range scheduleState.lagWindows {
		raiseTo(window, probe, lag)
	}
}

func raiseTo(m map[string]time.Duration, probe string, lag time.Duration) {
	if cur, ok := m[probe]; !ok || lag > cur {
		m[probe] = lag
	}
}

// TakeCollectLagWindow returns the max lag per probe since the previous call
// with the same reader, and resets that reader's maxima to zero while keeping
// the probe keys. Keeping keys matters for dashboards: a probe that missed
// this window must report 0, not disappear until its next lag sample. A
// reader's first call covers the time since the process started.
func TakeCollectLagWindow(reader string) map[string]float64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()

	window, ok := scheduleState.lagWindows[reader]
	if !ok {
		window = make(map[string]time.Duration, len(scheduleState.lagSinceStart))
		for probe, lag := range scheduleState.lagSinceStart {
			window[probe] = lag
		}
		scheduleState.lagWindows[reader] = window
	}
	out := make(map[string]float64, len(window))
	for probe, lag := range window {
		out[probe] = lag.Seconds()
		window[probe] = 0
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
	scheduleState.lagSinceStart = map[string]time.Duration{}
	scheduleState.lagWindows = map[string]map[string]time.Duration{}
	scheduleState.overruns = map[string]uint64{}
	scheduleState.firstCollectionDelay = map[string]time.Duration{}
}
