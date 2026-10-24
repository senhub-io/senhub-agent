package agentstate

import (
	"sync"
	"time"
)

// processStart is taken when the package is loaded, which for the agent is
// the start of the process. The first-collection delay is measured from it.
var processStart = time.Now()

// scheduleState keeps the process-wide probe scheduling counters. The lag
// maxima are windowed per reader: each exposition path (Prometheus, OTLP)
// reads the maximum since its own previous read, so one reader's read never
// resets what another reader is about to see.
//
// Every per-probe series is keyed by probe instance (configured name and
// type), so two probes of one type are two series.
var scheduleState = struct {
	mu sync.Mutex

	inFlight int64
	// lagSinceStart is the maximum lag per probe since the process
	// started. A reader seen for the first time starts from it, so its
	// first window is not blind to the collections that ran before it read.
	lagSinceStart        map[ProbeKey]time.Duration
	lagWindows           map[string]map[ProbeKey]time.Duration
	overruns             map[ProbeKey]uint64
	firstCollectionDelay map[ProbeKey]time.Duration
}{
	lagSinceStart:        map[ProbeKey]time.Duration{},
	lagWindows:           map[string]map[ProbeKey]time.Duration{},
	overruns:             map[ProbeKey]uint64{},
	firstCollectionDelay: map[ProbeKey]time.Duration{},
}

// ProbeKey identifies a probe instance: its configured name and its type.
type ProbeKey struct {
	Name string
	Type string
}

// ProcessStart returns the instant the process started.
func ProcessStart() time.Time {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	return processStart
}

// normalize replaces an empty name or type with the label used by the other
// agentstate probe-keyed metrics, so every scheduling metric shares the same
// bounded cardinality contract.
func (k ProbeKey) normalize() ProbeKey {
	if k.Name == "" {
		k.Name = "unknown"
	}
	if k.Type == "" {
		k.Type = "unknown"
	}
	return k
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
func RecordCollectLag(probe ProbeKey, lag time.Duration) {
	probe = probe.normalize()
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

func raiseTo(m map[ProbeKey]time.Duration, probe ProbeKey, lag time.Duration) {
	if cur, ok := m[probe]; !ok || lag > cur {
		m[probe] = lag
	}
}

// TakeCollectLagWindow returns the max lag per probe since the previous call
// with the same reader, and resets that reader's maxima to zero while keeping
// the probe keys. Keeping keys matters for dashboards: a probe that missed
// this window must report 0, not disappear until its next lag sample. A
// reader's first call covers the time since the process started.
func TakeCollectLagWindow(reader string) map[ProbeKey]float64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()

	window, ok := scheduleState.lagWindows[reader]
	if !ok {
		window = make(map[ProbeKey]time.Duration, len(scheduleState.lagSinceStart))
		for probe, lag := range scheduleState.lagSinceStart {
			window[probe] = lag
		}
		scheduleState.lagWindows[reader] = window
	}
	out := make(map[ProbeKey]float64, len(window))
	for probe, lag := range window {
		out[probe] = lag.Seconds()
		window[probe] = 0
	}
	return out
}

// IncrementCollectOverruns records scheduler ticks skipped because a previous
// collection of the same probe instance was still running. Non-positive ticks are
// ignored so callers don't have to guard a default zero value.
func IncrementCollectOverruns(probe ProbeKey, ticks int) {
	if ticks <= 0 {
		return
	}
	probe = probe.normalize()
	scheduleState.mu.Lock()
	scheduleState.overruns[probe] += uint64(ticks)
	scheduleState.mu.Unlock()
}

// GetCollectOverruns returns a snapshot copy of the cumulative overrun ticks
// per probe instance.
func GetCollectOverruns() map[ProbeKey]uint64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	out := make(map[ProbeKey]uint64, len(scheduleState.overruns))
	for probe, n := range scheduleState.overruns {
		out[probe] = n
	}
	return out
}

// RecordFirstCollectionEnd records, for a probe instance, the time from the
// process start to the end of its first collection. Only the first call per
// instance counts, so a reload of the same probe does not move the value
// later.
func RecordFirstCollectionEnd(probe ProbeKey, ended time.Time) {
	probe = probe.normalize()
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	if _, seen := scheduleState.firstCollectionDelay[probe]; seen {
		return
	}
	scheduleState.firstCollectionDelay[probe] = max(ended.Sub(processStart), 0)
}

// GetFirstCollectionDelays returns a snapshot copy of the first collection
// delay per probe instance, in seconds.
func GetFirstCollectionDelays() map[ProbeKey]float64 {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	out := make(map[ProbeKey]float64, len(scheduleState.firstCollectionDelay))
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
	scheduleState.lagSinceStart = map[ProbeKey]time.Duration{}
	scheduleState.lagWindows = map[string]map[ProbeKey]time.Duration{}
	scheduleState.overruns = map[ProbeKey]uint64{}
	scheduleState.firstCollectionDelay = map[ProbeKey]time.Duration{}
}

// SetProcessStartForTest replaces the process start instant.
func SetProcessStartForTest(t time.Time) {
	scheduleState.mu.Lock()
	defer scheduleState.mu.Unlock()
	processStart = t
}
