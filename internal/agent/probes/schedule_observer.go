package probes

import (
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

type scheduleObserver struct {
	probe string
	start atomic.Int64
}

func newScheduleObserver(probeType string) *scheduleObserver {
	return &scheduleObserver{probe: probeType}
}

func (o *scheduleObserver) markStart() {
	o.start.Store(time.Now().UnixNano())
}

func (o *scheduleObserver) CallStarted(lag time.Duration) {
	agentstate.CollectionStarted()
	agentstate.RecordCollectLag(o.probe, lag)
}

func (o *scheduleObserver) CallEnded(first bool) {
	agentstate.CollectionEnded()
	if first {
		if start := o.start.Load(); start != 0 {
			agentstate.RecordFirstCollectionDelay(o.probe, time.Since(time.Unix(0, start)))
		}
	}
}

func (o *scheduleObserver) CallOverran() {
	agentstate.IncrementCollectOverruns(o.probe, 1)
}
