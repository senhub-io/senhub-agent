package entity

import (
	"testing"
	"time"
)

type staticCadenceSource struct {
	obs     Observation
	cadence time.Duration
}

func (s staticCadenceSource) Observe() (Observation, bool) {
	return s.obs, true
}

func (s staticCadenceSource) Cadence() time.Duration {
	return s.cadence
}

func newTestDetector() (*Detector, *[]Event) {
	var events []Event
	d := NewDetector(
		func() (HostIdentity, error) { return HostIdentity{ID: "host-1"}, nil },
		func() AgentIdentity { return AgentIdentity{InstanceID: "agent-1"} },
		time.Minute,
	)
	d.publish = func(e Event) {
		events = append(events, e)
	}
	d.now = func() time.Time { return time.Unix(1000, 0) }
	return d, &events
}

func findStateEvent(events []Event, typ string, id map[string]any) (Event, bool) {
	for _, e := range events {
		if e.Kind == EntityState && e.Entity != nil && eventKey(e) == entityKey(typ, id) {
			return e, true
		}
	}
	return Event{}, false
}

func TestDetectorAnnouncesCadenceAndFallback(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	deviceID := map[string]any{"network.device.id": "d1"}
	routeID := map[string]any{"route.destination": "r1"}
	hostID := map[string]any{"host.id": "host-1"}

	RegisterSource(staticCadenceSource{
		obs: Observation{
			Entities: []Entity{{Type: "network.device", ID: deviceID}},
			Relations: []Relation{{
				Type:     "runs_on",
				FromType: "network.device",
				FromID:   deviceID,
				ToType:   "host",
				ToID:     hostID,
			}},
		},
		cadence: 10 * time.Minute,
	})
	RegisterSource(staticCadenceSource{
		obs: Observation{
			Entities: []Entity{{Type: "network.route", ID: routeID}},
			Relations: []Relation{{
				Type:     "has_route",
				FromType: "network.route",
				FromID:   routeID,
				ToType:   "network.device",
				ToID:     deviceID,
			}},
		},
	})

	d, events := newTestDetector()
	d.reconcile(NewTracker(d.publish, reEmitTicks*d.interval), d.now())

	dev, ok := findStateEvent(*events, "network.device", deviceID)
	if !ok {
		t.Fatal("network.device state event not published")
	}
	if dev.Interval != 10*time.Minute {
		t.Fatalf("device interval = %s, want %s", dev.Interval, 10*time.Minute)
	}

	route, ok := findStateEvent(*events, "network.route", routeID)
	if !ok {
		t.Fatal("network.route state event not published")
	}
	if route.Interval != time.Minute*livenessSlackFactor {
		t.Fatalf("route interval = %s, want %s", route.Interval, time.Minute*livenessSlackFactor)
	}
}

func TestDetectorDuplicateEntityTakesCoarsestCadence(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	deviceID := map[string]any{"network.device.id": "d1"}
	hostID := map[string]any{"host.id": "host-1"}
	obs := Observation{
		Entities: []Entity{{Type: "network.device", ID: deviceID}},
		Relations: []Relation{{
			Type:     "runs_on",
			FromType: "network.device",
			FromID:   deviceID,
			ToType:   "host",
			ToID:     hostID,
		}},
	}

	RegisterSource(staticCadenceSource{obs: obs, cadence: 10 * time.Minute})
	RegisterSource(staticCadenceSource{obs: obs})

	d, events := newTestDetector()
	d.reconcile(NewTracker(d.publish, reEmitTicks*d.interval), d.now())

	dev, ok := findStateEvent(*events, "network.device", deviceID)
	if !ok {
		t.Fatal("network.device state event not published")
	}
	if dev.Interval != 10*time.Minute {
		t.Fatalf("device interval = %s, want %s", dev.Interval, 10*time.Minute)
	}
}

func TestDetectorClampsAnnouncedCadence(t *testing.T) {
	ResetForTest()
	defer ResetForTest()

	deviceID := map[string]any{"network.device.id": "d1"}
	hostID := map[string]any{"host.id": "host-1"}
	RegisterSource(staticCadenceSource{
		obs: Observation{
			Entities: []Entity{{Type: "network.device", ID: deviceID}},
			Relations: []Relation{{
				Type:     "runs_on",
				FromType: "network.device",
				FromID:   deviceID,
				ToType:   "host",
				ToID:     hostID,
			}},
		},
		cadence: 30 * 24 * time.Hour,
	})

	d, events := newTestDetector()
	d.reconcile(NewTracker(d.publish, reEmitTicks*d.interval), d.now())

	dev, ok := findStateEvent(*events, "network.device", deviceID)
	if !ok {
		t.Fatal("network.device state event not published")
	}
	if dev.Interval != 30*24*time.Hour {
		t.Fatalf("device interval = %s, want the real 720h: never shortened below what the source reads", dev.Interval)
	}
}

// TestTrackerAnnouncedIntervalChangePublishesOnce checks a new cadence reaches
// the consumer at once, since it moves the liveness deadline, and only once.
func TestTrackerAnnouncedIntervalChangePublishesOnce(t *testing.T) {
	var events []Event
	tr := NewTracker(func(e Event) {
		events = append(events, e)
	}, time.Hour)
	now := time.Unix(1000, 0)
	id := map[string]any{"network.device.id": "d1"}

	entities := []Entity{{
		Type:       "network.device",
		ID:         id,
		Attributes: map[string]any{"sys.name": "switch-1"},
		Cadence:    10 * time.Minute,
	}}
	tr.Reconcile(stateEvents(entities, now, time.Minute*livenessSlackFactor), now, nil)

	entities[0].Cadence = 20 * time.Minute
	tr.Reconcile(stateEvents(entities, now, time.Minute*livenessSlackFactor), now, nil)
	tr.Reconcile(stateEvents(entities, now, time.Minute*livenessSlackFactor), now, nil)

	if len(events) != 2 || events[1].Interval != 20*time.Minute {
		t.Fatalf("got %d state events (%+v), want 2 with the second announcing 20m", len(events), events)
	}
}

type slowSource struct{ cadence time.Duration }

func (slowSource) Observe() (Observation, bool) { return Observation{}, true }
func (s slowSource) Cadence() time.Duration     { return s.cadence }

func TestSlowCadenceIsReportedOncePerValue(t *testing.T) {
	var seen []time.Duration
	d := &Detector{}
	d.OnCadenceBeyondCeiling(func(c time.Duration) { seen = append(seen, c) })
	month := 30 * 24 * time.Hour
	d.reportSlowCadence(1, slowSource{month})
	d.reportSlowCadence(1, slowSource{month})
	d.reportSlowCadence(2, slowSource{time.Hour})
	if len(seen) != 1 || seen[0] != month {
		t.Fatalf("reported %v, want the 30-day cadence once and nothing for 1h", seen)
	}
}
