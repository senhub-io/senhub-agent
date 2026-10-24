package probes

import (
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

type scheduleObserver struct {
	probe    string
	instance string
}

func newScheduleObserver(probeType, instance string) *scheduleObserver {
	return &scheduleObserver{probe: probeType, instance: instance}
}

func (o *scheduleObserver) CallStarted(lag time.Duration) {
	agentstate.CollectionStarted()
	agentstate.RecordCollectLag(o.probe, lag)
}

func (o *scheduleObserver) CallEnded(first bool) {
	agentstate.CollectionEnded()
	if first {
		agentstate.RecordFirstCollectionEnd(o.probe, o.instance, time.Now())
	}
}

func (o *scheduleObserver) CallOverran() {
	agentstate.IncrementCollectOverruns(o.probe, 1)
}
