package entity

import (
	"sync"
	"testing"
	"time"
)

// Startup hold (#625). A source that has not answered once yet (a slow first
// scan after a restart) must not let the agent's service.instance go out
// without that source's relations: the consumer would drop them, then
// re-create them minutes later. The service.instance alone waits, bounded in
// time; every other entity keeps its heartbeat.

const holdAgentID = "agent-7f3a"

// monitoredByAgent is a source's view: a database the agent monitors.
func monitoredByAgent() Observation {
	o := monitoredDB()
	o.Relations = append(o.Relations, Relation{
		Type:     "monitors",
		FromType: TypeServiceInstance,
		FromID:   map[string]any{"service.instance.id": holdAgentID},
		ToType:   "db",
		ToID:     dbTargetID,
	})
	return o
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) add(ev Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
}

// take returns the events published since the last call.
func (l *eventLog) take() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

func findEntity(events []Event, typ string) *Entity {
	for _, ev := range events {
		if ev.Entity != nil && ev.Entity.Type == typ {
			return ev.Entity
		}
	}
	return nil
}

func hasRelationship(e *Entity, typ, target string) bool {
	for _, r := range e.Relationships {
		if r.Type == typ && r.TargetType == target {
			return true
		}
	}
	return false
}

func TestDetector_AgentInstanceWaitsForASourceThatHasNotAnsweredYet(t *testing.T) {
	log := &eventLog{}
	tr := NewTracker(log.add, 0)
	d := newReasonDetector()
	src := &fakeSource{obs: monitoredByAgent(), ok: false}
	unregister := RegisterSource(src)
	defer unregister()
	t0 := time.Unix(1780272000, 0).UTC()

	d.reconcile(tr, t0)
	first := log.take()
	if findEntity(first, "host") == nil {
		t.Fatal("the host must keep its heartbeat during the hold")
	}
	if svc := findEntity(first, TypeServiceInstance); svc != nil {
		t.Fatalf("service.instance sent before the source answered, with %d relationships", len(svc.Relationships))
	}

	src.set(monitoredByAgent(), true)
	d.reconcile(tr, t0.Add(time.Minute))
	svc := findEntity(log.take(), TypeServiceInstance)
	if svc == nil {
		t.Fatal("service.instance not sent once every source answered")
	}
	if !hasRelationship(svc, "monitors", "db") {
		t.Fatalf("service.instance relationships = %+v, want the source's monitors edge", svc.Relationships)
	}
}

func TestDetector_AgentInstanceGoesOutAfterTheHoldEvenIfASourceNeverAnswers(t *testing.T) {
	log := &eventLog{}
	tr := NewTracker(log.add, 0)
	d := newReasonDetector()
	src := &fakeSource{obs: monitoredByAgent(), ok: false}
	unregister := RegisterSource(src)
	defer unregister()
	t0 := time.Unix(1780272000, 0).UTC()

	for i := 0; i < startupHoldTicks; i++ {
		d.reconcile(tr, t0.Add(time.Duration(i)*time.Minute))
		if svc := findEntity(log.take(), TypeServiceInstance); svc != nil {
			t.Fatalf("tick %d: service.instance sent during the hold", i)
		}
	}
	d.reconcile(tr, t0.Add(startupHoldTicks*time.Minute))
	if findEntity(log.take(), TypeServiceInstance) == nil {
		t.Fatal("service.instance still held after the bound")
	}
}
