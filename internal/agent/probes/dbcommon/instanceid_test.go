package dbcommon

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

// The defect this fixes, stated as a test: the same loopback endpoint on two
// machines must not produce the same identity. Without host-scoping both
// return "127.0.0.1:3306" and the consumer folds two databases into one.
func TestFallbackInstanceID_LoopbackIsUniquePerHost(t *testing.T) {
	a := FallbackInstanceID("127.0.0.1", 3306, "host-a")
	b := FallbackInstanceID("127.0.0.1", 3306, "host-b")
	if a == b {
		t.Fatalf("two hosts produced the same id %q — the collapse is back", a)
	}
	if a != "host-a:3306" {
		t.Errorf("id = %q, want host-a:3306", a)
	}
}

// Every spelling of "this machine" must scope, not just the numeric one: a
// probe configured with "localhost" and one configured with "127.0.0.1" watch
// the same database and must not disagree about its identity.
func TestFallbackInstanceID_EveryLocalSpellingScopes(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "localhost", "::1", "", "127.0.1.1"} {
		got := FallbackInstanceID(addr, 6379, "h1")
		if got != "h1:6379" {
			t.Errorf("address %q gave %q, want h1:6379", addr, got)
		}
	}
}

// A routable address already distinguishes the target. Rewriting it would
// re-key every remote database in the graph for nothing.
func TestFallbackInstanceID_RemoteAddressIsUntouched(t *testing.T) {
	if got := FallbackInstanceID("10.0.0.5", 5432, "h1"); got != "10.0.0.5:5432" {
		t.Errorf("remote id = %q, want 10.0.0.5:5432", got)
	}
	if got := FallbackInstanceID("db.internal", 5432, "h1"); got != "db.internal:5432" {
		t.Errorf("named remote id = %q, want db.internal:5432", got)
	}
}

// When the host id cannot be read, the id keeps its historical shape. Emitting
// a differently-shaped id on a host whose identity lookup failed would re-key
// that database every time the lookup flapped.
func TestFallbackInstanceID_NoHostIDKeepsTheRawForm(t *testing.T) {
	if got := FallbackInstanceID("127.0.0.1", 3306, ""); got != "127.0.0.1:3306" {
		t.Errorf("id = %q, want the unscoped 127.0.0.1:3306", got)
	}
}

// The scoped id must clear the collapse guard in entity.LocalRunsOn, which
// refuses an anchor from an identity embedding the loopback literal. Before
// this change a local database could not be attached to its host at all; the
// guard was doing its job on an identity that should never have existed.
func TestFallbackInstanceID_ScopedIDCanAnchorToItsHost(t *testing.T) {
	scoped := FallbackInstanceID("127.0.0.1", 3306, "h1")
	id := map[string]any{"db.instance.id": scoped}

	rel, ok := entity.LocalRunsOn("db", id, "127.0.0.1", "h1")
	if !ok {
		t.Fatal("a host-scoped local db still cannot anchor to its host")
	}
	if rel.ToID["host.id"] != "h1" {
		t.Errorf("anchored to %v, want h1", rel.ToID["host.id"])
	}

	// And the unscoped form must still be refused, so the guard is not weakened.
	raw := map[string]any{"db.instance.id": "127.0.0.1:3306"}
	if _, ok := entity.LocalRunsOn("db", raw, "127.0.0.1", "h1"); ok {
		t.Error("the loopback-derived identity was anchored — the guard is gone")
	}
}

// The scoped shape must match what service.listener mints for the same idea,
// so one machine's local services are spelled the same way across types.
func TestFallbackInstanceID_MatchesTheListenerShape(t *testing.T) {
	got := FallbackInstanceID("127.0.0.1", 9467, "abc-123")
	if !strings.HasPrefix(got, "abc-123:") {
		t.Errorf("id = %q, want the <host.id>:<port> shape service.listener uses", got)
	}
}
