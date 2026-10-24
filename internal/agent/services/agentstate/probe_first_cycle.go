package agentstate

import "strings"

// probeEventDriven holds the probes whose data arrives through a callback
// or a listener. Guarded by probeStateMu.
var probeEventDriven = map[string]struct{}{}

// firstCycleSignals holds, per probe ID, a channel closed by the probe's
// first RecordProbeCycle. Guarded by probeStateMu.
var firstCycleSignals = map[string]chan struct{}{}

// MarkProbeEventDriven records that the probe's data arrives through a
// callback or a listener, so nothing is worth waiting for after a start.
func MarkProbeEventDriven(probeID string) {
	probeStateMu.Lock()
	probeEventDriven[probeID] = struct{}{}
	probeStateMu.Unlock()
}

// FirstCycleSignalByName returns a channel closed once the running probe
// of that configured name has recorded its first cycle, whatever it
// delivered; already closed if it has. ok is false for a name no running
// probe carries.
func FirstCycleSignalByName(name string) (ch <-chan struct{}, ok bool) {
	probeStateMu.Lock()
	defer probeStateMu.Unlock()
	id, ok := activeProbeByName[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	return firstCycleSignalLocked(id), true
}

func firstCycleSignalLocked(probeID string) chan struct{} {
	ch, ok := firstCycleSignals[probeID]
	if !ok {
		ch = make(chan struct{})
		firstCycleSignals[probeID] = ch
		if _, done := probeCycles[probeID]; done {
			close(ch)
		}
	}
	return ch
}

func closeFirstCycleSignalLocked(probeID string) {
	ch := firstCycleSignalLocked(probeID)
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func pruneProbeKindLocked(active map[string]struct{}) {
	for id := range probeEventDriven {
		if _, alive := active[id]; !alive {
			delete(probeEventDriven, id)
		}
	}
	for id := range firstCycleSignals {
		if _, alive := active[id]; !alive {
			delete(firstCycleSignals, id)
		}
	}
}
