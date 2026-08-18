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
	a := FallbackInstanceID("mysql", "127.0.0.1", 3306, "host-a")
	b := FallbackInstanceID("mysql", "127.0.0.1", 3306, "host-b")
	if a == b {
		t.Fatalf("two hosts produced the same id %q — the collapse is back", a)
	}
	if a != "mysql:3306@host-a" {
		t.Errorf("id = %q, want mysql:3306@host-a", a)
	}
}

// Every spelling of "this machine" must scope, not just the numeric one: a
// probe configured with "localhost" and one configured with "127.0.0.1" watch
// the same database and must not disagree about its identity.
func TestFallbackInstanceID_EveryLocalSpellingScopes(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "localhost", "::1", "", "127.0.1.1"} {
		got := FallbackInstanceID("redis", addr, 6379, "h1")
		if got != "redis:6379@h1" {
			t.Errorf("address %q gave %q, want redis:6379@h1", addr, got)
		}
	}
}

// A routable address already distinguishes the target. Rewriting it would
// re-key every remote database in the graph for nothing.
func TestFallbackInstanceID_RemoteAddressIsUntouched(t *testing.T) {
	if got := FallbackInstanceID("postgresql", "10.0.0.5", 5432, "h1"); got != "10.0.0.5:5432" {
		t.Errorf("remote id = %q, want 10.0.0.5:5432", got)
	}
	if got := FallbackInstanceID("postgresql", "db.internal", 5432, "h1"); got != "db.internal:5432" {
		t.Errorf("named remote id = %q, want db.internal:5432", got)
	}
}

// When the host id cannot be read, the id keeps its historical shape. Emitting
// a differently-shaped id on a host whose identity lookup failed would re-key
// that database every time the lookup flapped.
func TestFallbackInstanceID_NoHostIDKeepsTheRawForm(t *testing.T) {
	if got := FallbackInstanceID("mysql", "127.0.0.1", 3306, ""); got != "127.0.0.1:3306" {
		t.Errorf("id = %q, want the unscoped 127.0.0.1:3306", got)
	}
}

// The scoped id must clear the collapse guard in entity.LocalRunsOn, which
// refuses an anchor from an identity embedding the loopback literal. Before
// this change a local database could not be attached to its host at all; the
// guard was doing its job on an identity that should never have existed.
func TestFallbackInstanceID_ScopedIDCanAnchorToItsHost(t *testing.T) {
	scoped := FallbackInstanceID("mysql", "127.0.0.1", 3306, "h1")
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

// The db identity must NOT be mistakable for the service.listener minted on
// the same socket.
//
// The first version of this fix deliberately copied the listener shape
// (<host.id>:<port>), on the theory that one machine's local things should be
// spelled alike. The topology consumer pushed back and was right: the two
// strings were never equal — a listener carries a /<transport> suffix — but
// two identities differing only by a suffix invite a reader to conclude they
// name the same thing. Naming the system removes the invitation.
func TestFallbackInstanceID_IsNotMistakableForAListener(t *testing.T) {
	db := FallbackInstanceID("mysql", "127.0.0.1", 3306, "abc-123")
	listener := "abc-123:3306/tcp" // the form hostsvc mints for the same socket

	if db == listener {
		t.Fatalf("db and listener identities are equal: %q", db)
	}
	if strings.HasPrefix(db, "abc-123:") {
		t.Errorf("db id %q still opens like a listener id; the system name must lead", db)
	}
	if !strings.HasPrefix(db, "mysql:") {
		t.Errorf("db id = %q, want it to open with the system name", db)
	}
	if !strings.HasSuffix(db, "@abc-123") {
		t.Errorf("db id = %q, want the @<host.id> form the contract prescribes", db)
	}
}

// The system name is required for the scoped form: without it there is nothing
// to distinguish the id, so the raw address is kept rather than minting
// ":3306@<host.id>", which would name a port on a host and nothing else.
func TestFallbackInstanceID_NoSystemKeepsTheRawForm(t *testing.T) {
	if got := FallbackInstanceID("", "127.0.0.1", 3306, "h1"); got != "127.0.0.1:3306" {
		t.Errorf("id = %q, want the unscoped 127.0.0.1:3306", got)
	}
}
