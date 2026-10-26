package probes

import (
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

type scheduleObserver struct {
	probe agentstate.ProbeKey
}

func newScheduleObserver(probeName, probeType string) *scheduleObserver {
	return &scheduleObserver{probe: agentstate.ProbeKey{Name: probeName, Type: probeType}}
}

func (o *scheduleObserver) CallStarted(lag time.Duration) {
	agentstate.CollectionStarted()
	agentstate.RecordCollectLag(o.probe, lag)
}

func (o *scheduleObserver) CallEnded(first bool) {
	agentstate.CollectionEnded()
	if first {
		agentstate.RecordFirstCollectionEnd(o.probe, time.Now())
	}
}

func (o *scheduleObserver) CallOverran() {
	agentstate.IncrementCollectOverruns(o.probe, 1)
}
