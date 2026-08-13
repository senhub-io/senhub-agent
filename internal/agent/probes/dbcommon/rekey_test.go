package dbcommon

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// A remote target was never re-keyed, so there is nothing to retire and no
// alias to draw. Announcing one would tell the consumer a node died that never
// existed under that name.
func TestRekeyAnnouncer_NilForARemoteTarget(t *testing.T) {
	if a := NewRekeyAnnouncer("mysql", "10.0.0.5", 3306, "h1"); a != nil {
		t.Fatalf("announcer built for a remote target: %+v", a)
	}
	// Same when the host id is unreadable: the identity did not change either.
	if a := NewRekeyAnnouncer("mysql", "127.0.0.1", 3306, ""); a != nil {
		t.Fatalf("announcer built when nothing was scoped: %+v", a)
	}
}

// The delete must name the OLD identity — the node the consumer actually has —
// and the alias must point from the new one to it.
func TestRekeyAnnouncer_RetiresTheOldIdentityAndAliasesIt(t *testing.T) {
	a := NewRekeyAnnouncer("mysql", "127.0.0.1", 3306, "h1")
	if a == nil {
		t.Fatal("no announcer for a host-scoped local db")
	}

	ch := entity.SubscribeEvents(8)
	defer entity.UnsubscribeEvents(ch)

	a.Announce()

	select {
	case ev := <-ch:
		if ev.Kind != entity.EntityDelete {
			t.Fatalf("published a %v, want a delete", ev.Kind)
		}
		if got := ev.Entity.ID["db.instance.id"]; got != "127.0.0.1:3306" {
			t.Errorf("delete names %q, want the pre-0.5.4 identity 127.0.0.1:3306", got)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing was published")
	}

	rel, ok := a.SameAs()
	if !ok {
		t.Fatal("no alias edge during the announcement window")
	}
	if rel.Type != entity.RelSameAs {
		t.Errorf("edge type = %q, want same_as", rel.Type)
	}
	if got := rel.FromID["db.instance.id"]; got != "mysql:3306@h1" {
		t.Errorf("edge starts at %q, want the new identity", got)
	}
	if got := rel.ToID["db.instance.id"]; got != "127.0.0.1:3306" {
		t.Errorf("edge points at %q, want the retired identity", got)
	}
	// Without these the consumer treats the alias as inert: it would travel the
	// whole wire and collapse nothing.
	if rel.Attributes["basis"] != "rekey" {
		t.Errorf("basis = %v, want rekey", rel.Attributes["basis"])
	}
	if rel.Attributes["confidence"] != 1.0 {
		t.Errorf("confidence = %v, want 1.0", rel.Attributes["confidence"])
	}
}

// The announcement is a migration signal, not a heartbeat: it repeats a few
// times to survive a late subscriber or a dropped event, then stops for good.
func TestRekeyAnnouncer_StopsAfterItsWindow(t *testing.T) {
	a := NewRekeyAnnouncer("redis", "localhost", 6379, "h1")
	if a == nil {
		t.Fatal("no announcer")
	}

	ch := entity.SubscribeEvents(32)
	defer entity.UnsubscribeEvents(ch)

	for i := 0; i < rekeyAnnounceCycles+4; i++ {
		a.Announce()
	}

	published := 0
	for {
		select {
		case <-ch:
			published++
			continue
		case <-time.After(50 * time.Millisecond):
		}
		break
	}
	if published != rekeyAnnounceCycles {
		t.Errorf("published %d deletes, want exactly %d", published, rekeyAnnounceCycles)
	}
	if _, ok := a.SameAs(); ok {
		t.Error("the alias edge is still offered after the window closed")
	}
}
