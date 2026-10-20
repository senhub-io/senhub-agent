package entity

import (
	"os"
	"strings"
	"testing"
)

func routeEntity(host, dest string) Entity {
	return Entity{Type: "network.route", ID: map[string]any{"host.id": host, "dest": dest}}
}

func legacyRouteID(host, dest string) map[string]any {
	return map[string]any{"host.id": host, "route.destination": dest}
}

func routeLegacyFunc(e Entity) (string, map[string]any, bool) {
	if e.Type != "network.route" {
		return "", nil, false
	}
	host, _ := e.ID["host.id"].(string)
	dest, _ := e.ID["dest"].(string)
	if host == "" || dest == "" {
		return "", nil, false
	}
	return "network.route", legacyRouteID(host, dest), true
}

func sameLegacyEntity(host, leaf string) Entity {
	return Entity{Type: "thing", ID: map[string]any{"host.id": host, "leaf": leaf}}
}

func sameLegacyFunc(e Entity) (string, map[string]any, bool) {
	if e.Type != "thing" {
		return "", nil, false
	}
	host, _ := e.ID["host.id"].(string)
	if host == "" {
		return "", nil, false
	}
	return "thing", map[string]any{"host.id": host}, true
}

func ensureNoRekeyMarkers(t *testing.T) {
	t.Helper()
	EnableRekeyMarkers("", nil)
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })
}

func drainEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var events []Event
	for {
		select {
		case ev := <-ch:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func countDeleteEvents(events []Event) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == EntityDelete {
			n++
		}
	}
	return n
}

func countRekeyMarkers(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".announced") {
			n++
		}
	}
	return n
}

func TestSetRekeyAnnouncer_WindowAndRepeats(t *testing.T) {
	ensureNoRekeyMarkers(t)
	entities := []Entity{
		routeEntity("h1", "d1"),
		routeEntity("h2", "d2"),
		routeEntity("h3", "d3"),
	}
	ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)

	ResetForTest()
	ch := SubscribeEvents(256)

	relationCount := 0
	for cycle := 0; cycle < 6; cycle++ {
		ann.BeginCycle()
		got := 0
		for _, e := range entities {
			rel, ok := ann.Announce(e)
			if !ok {
				continue
			}
			got++
			relationCount++

			legacyType, legacyID, ok := routeLegacyFunc(e)
			if !ok {
				t.Fatalf("cycle %d: derive failed for %v", cycle, e)
			}
			if rel.Type != RelSameAs {
				t.Fatalf("cycle %d: relation type = %v, want RelSameAs", cycle, rel.Type)
			}
			if rel.FromType != e.Type || !idMapsEqual(rel.FromID, e.ID) {
				t.Fatalf("cycle %d: relation from = %v %v, want %v %v", cycle, rel.FromType, rel.FromID, e.Type, e.ID)
			}
			if rel.ToType != legacyType || !idMapsEqual(rel.ToID, legacyID) {
				t.Fatalf("cycle %d: relation to = %v %v, want %v %v", cycle, rel.ToType, rel.ToID, legacyType, legacyID)
			}
			if basis, ok := rel.Attributes["basis"].(string); !ok || basis != "rekey" {
				t.Fatalf("cycle %d: basis = %v, want rekey", cycle, rel.Attributes["basis"])
			}
			if confidence, ok := rel.Attributes["confidence"].(float64); !ok || confidence != 1.0 {
				t.Fatalf("cycle %d: confidence = %v, want 1.0", cycle, rel.Attributes["confidence"])
			}
		}
		if cycle < RekeyAnnounceCycles {
			if got != len(entities) {
				t.Fatalf("cycle %d: got %d relations, want %d", cycle, got, len(entities))
			}
		} else {
			if got != 0 {
				t.Fatalf("cycle %d: got %d relations, want 0 after window closed", cycle, got)
			}
		}
	}

	if relationCount != 9 {
		t.Fatalf("total relations = %d, want 9", relationCount)
	}
	events := drainEvents(t, ch)
	if got := countDeleteEvents(events); got != 9 {
		t.Fatalf("deletes = %d, want 9", got)
	}
	for _, ev := range events {
		if ev.Kind == EntityDelete && ev.DeleteReason != ReasonRekeyed {
			t.Fatalf("delete reason = %v, want ReasonRekeyed", ev.DeleteReason)
		}
	}
}

func TestSetRekeyAnnouncer_MarkerSuppressionAcrossRestart(t *testing.T) {
	EnableRekeyMarkers("", nil)
	dir := t.TempDir()
	EnableRekeyMarkers(dir, nil)
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })

	entities := []Entity{routeEntity("h1", "d1")}

	first := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	got := countRekeyDeletes(t, func() {
		for i := 0; i < 5; i++ {
			first.BeginCycle()
			for _, e := range entities {
				first.Announce(e)
			}
		}
		first.Close()
	})
	if got != RekeyAnnounceCycles {
		t.Fatalf("first start deletes = %d, want %d", got, RekeyAnnounceCycles)
	}

	second := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	got = countRekeyDeletes(t, func() {
		second.BeginCycle()
		for _, e := range entities {
			rel, ok := second.Announce(e)
			if ok {
				t.Fatal("second start with same name must be suppressed")
			}
			if rel.Type != "" {
				t.Fatalf("suppressed relation = %v, want zero relation", rel)
			}
		}
		second.Close()
	})
	if got != 0 {
		t.Fatalf("second start deletes = %d, want 0", got)
	}

	third := NewSetRekeyAnnouncer("routes-2", routeLegacyFunc, 10, nil)
	got = countRekeyDeletes(t, func() {
		for i := 0; i < 5; i++ {
			third.BeginCycle()
			for _, e := range entities {
				third.Announce(e)
			}
		}
		third.Close()
	})
	if got != RekeyAnnounceCycles {
		t.Fatalf("different name deletes = %d, want %d", got, RekeyAnnounceCycles)
	}

	EnableRekeyMarkers("", nil)
	fourth := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	got = countRekeyDeletes(t, func() {
		for i := 0; i < 5; i++ {
			fourth.BeginCycle()
			for _, e := range entities {
				fourth.Announce(e)
			}
		}
		fourth.Close()
	})
	if got != RekeyAnnounceCycles {
		t.Fatalf("disabled marker store deletes = %d, want %d", got, RekeyAnnounceCycles)
	}
}

func TestSetRekeyAnnouncer_MarkerWrittenOnlyAfterWindowClosed(t *testing.T) {
	EnableRekeyMarkers("", nil)
	dir := t.TempDir()
	EnableRekeyMarkers(dir, nil)
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })

	ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	e := routeEntity("h1", "d1")

	run := func() {
		t.Helper()
		ann.BeginCycle()
		if _, ok := ann.Announce(e); !ok {
			t.Fatal("expected announcement before window closed")
		}
	}

	run()
	run()
	if n := countRekeyMarkers(t, dir); n != 0 {
		t.Fatalf("markers after two cycles = %d, want 0", n)
	}

	run()
	ann.Close()
	if n := countRekeyMarkers(t, dir); n != 1 {
		t.Fatalf("markers after third cycle + Close = %d, want 1", n)
	}
}

func TestSetRekeyAnnouncer_Bound(t *testing.T) {
	ensureNoRekeyMarkers(t)
	ResetForTest()
	ch := SubscribeEvents(256)

	warnCount := 0
	ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 2, func(tracked int) {
		warnCount++
		if tracked != 2 {
			t.Errorf("warn tracked = %d, want 2", tracked)
		}
	})

	entities := []Entity{
		routeEntity("h1", "d1"),
		routeEntity("h2", "d2"),
		routeEntity("h3", "d3"),
		routeEntity("h4", "d4"),
		routeEntity("h5", "d5"),
	}

	for cycle := 0; cycle < 3; cycle++ {
		ann.BeginCycle()
		got := 0
		for _, e := range entities {
			if _, ok := ann.Announce(e); ok {
				got++
			}
		}
		if got != 2 {
			t.Fatalf("cycle %d: announced %d entities, want 2", cycle, got)
		}
	}

	// The next BeginCycle closes the window after the previous announced cycle.
	ann.BeginCycle()
	for _, e := range entities {
		if _, ok := ann.Announce(e); ok {
			t.Fatalf("announcement after window closed for %v", e)
		}
	}

	if warnCount != 1 {
		t.Fatalf("warn called %d times, want 1", warnCount)
	}
	events := drainEvents(t, ch)
	if got := countDeleteEvents(events); got != 6 {
		t.Fatalf("deletes = %d, want 6", got)
	}
}

func TestSetRekeyAnnouncer_EntityAppearingLate(t *testing.T) {
	ensureNoRekeyMarkers(t)
	ResetForTest()
	ch := SubscribeEvents(256)

	ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	eA := routeEntity("hA", "dA")
	eB := routeEntity("hB", "dB")
	eC := routeEntity("hC", "dC")

	for cycle := 0; cycle < 5; cycle++ {
		ann.BeginCycle()

		if _, ok := ann.Announce(eA); ok {
			if cycle >= RekeyAnnounceCycles {
				t.Fatalf("cycle %d: A announced after window closed", cycle)
			}
		} else {
			if cycle < RekeyAnnounceCycles {
				t.Fatalf("cycle %d: A did not announce before window closed", cycle)
			}
		}

		if cycle >= 2 {
			if _, ok := ann.Announce(eB); ok {
				if cycle != 2 {
					t.Fatalf("cycle %d: B announced, want only cycle 2", cycle)
				}
			} else {
				if cycle == 2 {
					t.Fatalf("cycle %d: B did not announce when first seen", cycle)
				}
			}
		}

		if cycle >= 3 {
			if _, ok := ann.Announce(eC); ok {
				t.Fatalf("cycle %d: C announced after window closed", cycle)
			}
		}
	}

	events := drainEvents(t, ch)
	countA, countB, countC := 0, 0, 0
	for _, ev := range events {
		if ev.Kind != EntityDelete {
			continue
		}
		if ev.DeleteReason != ReasonRekeyed {
			t.Fatalf("delete reason = %v, want ReasonRekeyed", ev.DeleteReason)
		}
		switch {
		case idMapsEqual(ev.Entity.ID, legacyRouteID("hA", "dA")):
			countA++
		case idMapsEqual(ev.Entity.ID, legacyRouteID("hB", "dB")):
			countB++
		case idMapsEqual(ev.Entity.ID, legacyRouteID("hC", "dC")):
			countC++
		default:
			t.Fatalf("unexpected delete id %v", ev.Entity.ID)
		}
	}

	if countA != 3 || countB != 1 || countC != 0 {
		t.Fatalf("delete counts: A=%d, B=%d, C=%d; want A=3, B=1, C=0", countA, countB, countC)
	}
}

func TestSetRekeyAnnouncer_IdenticalLegacyIdsDeduplicated(t *testing.T) {
	ensureNoRekeyMarkers(t)

	e1 := sameLegacyEntity("h", "a")
	e2 := sameLegacyEntity("h", "b")

	run := func(entities []Entity) (deletes int, firstFromID map[string]any) {
		t.Helper()
		ResetForTest()
		ch := SubscribeEvents(256)
		ann := NewSetRekeyAnnouncer("same", sameLegacyFunc, 10, nil)

		seenFirst := false
		for cycle := 0; cycle < 3; cycle++ {
			ann.BeginCycle()
			got := 0
			for _, e := range entities {
				rel, ok := ann.Announce(e)
				if !ok {
					continue
				}
				got++
				if !seenFirst {
					seenFirst = true
					firstFromID = cloneID(rel.FromID)
				}
			}
			if got != 1 {
				t.Fatalf("cycle %d: got %d relations, want 1", cycle, got)
			}
		}

		events := drainEvents(t, ch)
		deletes = countDeleteEvents(events)
		return deletes, firstFromID
	}

	deletes1, first1 := run([]Entity{e1, e2})
	if deletes1 != 3 {
		t.Fatalf("first order deletes = %d, want 3", deletes1)
	}
	if !idMapsEqual(first1, e1.ID) {
		t.Fatalf("first order claimant from id = %v, want %v", first1, e1.ID)
	}

	deletes2, first2 := run([]Entity{e2, e1})
	if deletes2 != 3 {
		t.Fatalf("second order deletes = %d, want 3", deletes2)
	}
	if !idMapsEqual(first2, e2.ID) {
		t.Fatalf("second order claimant from id = %v, want %v", first2, e2.ID)
	}
}

func TestSetRekeyAnnouncer_EmptyCyclesDoNotConsumeWindow(t *testing.T) {
	ensureNoRekeyMarkers(t)
	e := routeEntity("h1", "d1")

	deletes := countRekeyDeletes(t, func() {
		ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)

		for i := 0; i < 4; i++ {
			ann.BeginCycle()
		}
		for i := 0; i < 3; i++ {
			ann.BeginCycle()
			if _, ok := ann.Announce(e); !ok {
				t.Fatal("expected announcement after empty cycles")
			}
		}
		ann.Close()
	})

	if deletes != 3 {
		t.Fatalf("deletes = %d, want 3", deletes)
	}
}

func TestSetRekeyAnnouncer_DeriveIgnoredNoEventNoMarker(t *testing.T) {
	EnableRekeyMarkers("", nil)
	dir := t.TempDir()
	EnableRekeyMarkers(dir, nil)
	t.Cleanup(func() { EnableRekeyMarkers("", nil) })

	derive := func(e Entity) (string, map[string]any, bool) {
		switch e.Type {
		case "ignored":
			return "", nil, false
		case "same":
			return e.Type, cloneID(e.ID), true
		default:
			return "", nil, false
		}
	}

	ann := NewSetRekeyAnnouncer("ignore", derive, 10, nil)
	ResetForTest()
	ch := SubscribeEvents(64)

	ann.BeginCycle()
	if _, ok := ann.Announce(Entity{Type: "ignored", ID: map[string]any{"host.id": "h"}}); ok {
		t.Fatal("ignored entity must not announce")
	}
	if _, ok := ann.Announce(Entity{Type: "same", ID: map[string]any{"host.id": "h"}}); ok {
		t.Fatal("identity equal to current must not announce")
	}
	ann.Close()

	events := drainEvents(t, ch)
	if got := countDeleteEvents(events); got != 0 {
		t.Fatalf("deletes = %d, want 0", got)
	}
	if n := countRekeyMarkers(t, dir); n != 0 {
		t.Fatalf("markers = %d, want 0", n)
	}
}

func TestSetRekeyAnnouncer_NilReceiverAndInvalidConstructor(t *testing.T) {
	ensureNoRekeyMarkers(t)

	var ann *SetRekeyAnnouncer
	ann.BeginCycle()
	ann.Close()
	if rel, ok := ann.Announce(Entity{Type: "x", ID: map[string]any{"id": "1"}}); ok || rel.Type != "" {
		t.Fatalf("nil receiver Announce = (%v, %v), want zero relation and false", rel, ok)
	}

	if got := NewSetRekeyAnnouncer("routes", nil, 10, nil); got != nil {
		t.Fatalf("nil derive returned %v, want nil", got)
	}
	if got := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 0, nil); got != nil {
		t.Fatalf("maxEntities 0 returned %v, want nil", got)
	}
	if got := NewSetRekeyAnnouncer("routes", routeLegacyFunc, -1, nil); got != nil {
		t.Fatalf("negative maxEntities returned %v, want nil", got)
	}
}

func TestSetRekeyAnnouncer_DuplicateAnnounceSameCycle(t *testing.T) {
	ensureNoRekeyMarkers(t)
	ResetForTest()
	ch := SubscribeEvents(64)

	ann := NewSetRekeyAnnouncer("routes", routeLegacyFunc, 10, nil)
	e := routeEntity("h1", "d1")

	ann.BeginCycle()
	if _, ok := ann.Announce(e); !ok {
		t.Fatal("first announce should succeed")
	}
	if rel, ok := ann.Announce(e); ok || rel.Type != "" {
		t.Fatalf("second announce = (%v, %v), want zero relation and false", rel, ok)
	}
	ann.Close()

	events := drainEvents(t, ch)
	if got := countDeleteEvents(events); got != 1 {
		t.Fatalf("deletes = %d, want 1", got)
	}
}
