package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Announcing an identity change so it does not read as an outage.
//
// When a producer corrects an identity, the consumer does not see a corrected
// value: it sees the old node stop being fed and a new one appear, with nothing
// linking them. Left alone the old one dies of liveness expiry, which their
// change feed reports as "the producer went quiet" — so a fleet-wide re-key
// produces the exact signature of a mass outage, on the feed people use to
// triage incidents.
//
// Agreed with the topology consumer (2026-08-13): the producer says it
// explicitly instead. Two announcements, both unconditional:
//
//   - an entity.delete on the OLD identity, so the retirement carries
//     delete_source=producer — someone decided, rather than someone stopped
//     answering;
//   - a same_as edge from the new identity to the old one, so both timelines
//     stay joinable forever without anything being rewritten.
//
// Unconditional includes a fresh install where the old entity never existed:
// their engine resolves the identity first and returns without doing anything
// when nothing matches, and an orphan alias parks in their reconciliation
// buffer then drops with one log line. They preferred absorbing that to having
// a producer persist state that would need maintaining, migrating and
// re-justifying every six months.
type RekeyAnnouncer struct {
	legacyType string
	legacyID   map[string]any
	newType    string
	newID      map[string]any
	// announced counts emissions. The delete repeats for the first few cycles
	// rather than firing once at startup: PublishEvent is best-effort and drops
	// under backpressure, and at process start the entity pump may not have
	// subscribed yet, so a single shot can vanish in silence. Repetition is free
	// because the consumer's delete is idempotent by contract — written that way
	// for a producer that restarts.
	announced atomic.Int32
	// checked records that the marker of an earlier start was looked for; it
	// only latches once a marker store is active, so a look made before
	// EnableRekeyMarkers does not hide a marker that exists. suppressed is set
	// when that earlier start already announced this re-key.
	checkMu    sync.Mutex
	checked    bool
	suppressed atomic.Bool
}

// markerStore remembers, across restarts, which re-keys were announced. The
// consumer reads every delete as "the producer says it is gone", so repeating
// the announcement at each start would retract the legacy node forever. One
// small file per re-key lives in the state directory; a start that finds it
// stays silent.
type markerStore struct {
	dir      string
	warn     func(error)
	warnOnce sync.Once
}

var markers atomic.Pointer[markerStore]

// EnableRekeyMarkers makes announcers announce once per state directory
// instead of at every process start. warn receives the first failure to
// write a marker (the announcement is then repeated at the next start, as
// before). An empty dir restores the stateless behaviour.
func EnableRekeyMarkers(dir string, warn func(error)) {
	if dir == "" {
		markers.Store(nil)
		return
	}
	markers.Store(&markerStore{dir: dir, warn: warn})
}

func identityPayload(typ string, id map[string]any) string {
	payload, _ := json.Marshal(struct {
		Type string         `json:"type"`
		ID   map[string]any `json:"id"`
	}{Type: typ, ID: id})
	return string(payload)
}

func (m *markerStore) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(m.dir, "rekey-"+hex.EncodeToString(sum[:8])+".announced")
}

// markerPresent reports whether an earlier start already wrote the marker
// for key. The second result is false while no marker store is active.
func markerPresent(key string) (present, active bool) {
	m := markers.Load()
	if m == nil {
		return false, false
	}
	_, err := os.Stat(m.path(key))
	return err == nil, true
}

// markerWrite records that the re-key named by key was announced. The first
// failure is handed to the store's warn callback; later ones are dropped.
func markerWrite(key, content string) {
	m := markers.Load()
	if m == nil {
		return
	}
	err := os.MkdirAll(m.dir, 0o755)
	if err == nil {
		err = os.WriteFile(m.path(key), []byte(content+"\n"), 0o644)
	}
	if err != nil && m.warn != nil {
		m.warnOnce.Do(func() { m.warn(err) })
	}
}

func (r *RekeyAnnouncer) checkMarker() {
	r.checkMu.Lock()
	defer r.checkMu.Unlock()
	if r.checked {
		return
	}
	present, active := markerPresent(r.markerKey())
	if !active {
		return
	}
	r.checked = true
	if present {
		r.suppressed.Store(true)
	}
}

func (r *RekeyAnnouncer) writeMarker() {
	markerWrite(r.markerKey(), identityPayload(r.legacyType, r.legacyID)+" -> "+identityPayload(r.newType, r.newID))
}

// RekeyAnnounceCycles is how many observation cycles carry the announcement.
// Three covers a late subscriber and a dropped event without turning a one-off
// migration signal into a recurring one.
const RekeyAnnounceCycles = 3

func cloneID(id map[string]any) map[string]any {
	if id == nil {
		return nil
	}
	out := make(map[string]any, len(id))
	for k, v := range id {
		out[k] = v
	}
	return out
}

func idMapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av != bv {
			return false
		}
	}
	return true
}

// NewRekeyAnnouncer returns an announcer for an identity that changed, or nil
// when the two spellings are equal — nothing was re-keyed, so there is no node
// to retire and no alias to draw. Returning nil rather than an inert object
// keeps "did anything change" answerable at the call site.
func NewRekeyAnnouncer(entityType, idKey, legacyID, newID string) *RekeyAnnouncer {
	if legacyID == "" || newID == "" || legacyID == newID {
		return nil
	}
	return NewRekeyAnnouncerWithIDs(
		entityType,
		map[string]any{idKey: legacyID},
		entityType,
		map[string]any{idKey: newID},
	)
}

// NewRekeyAnnouncerWithIDs returns an announcer for identities that changed,
// including identities of different types with multi-key ids. It returns nil
// when either identity is empty or the two spellings are equal.
func NewRekeyAnnouncerWithIDs(legacyType string, legacyID map[string]any, newType string, newID map[string]any) *RekeyAnnouncer {
	if len(legacyID) == 0 || len(newID) == 0 {
		return nil
	}
	if legacyType == newType && idMapsEqual(legacyID, newID) {
		return nil
	}
	return &RekeyAnnouncer{
		legacyType: legacyType,
		legacyID:   cloneID(legacyID),
		newType:    newType,
		newID:      cloneID(newID),
	}
}

// Announce publishes the delete of the old identity. Call it once per
// observation cycle; it stops on its own after RekeyAnnounceCycles.
func (r *RekeyAnnouncer) Announce() {
	if r == nil {
		return
	}
	r.checkMarker()
	if r.suppressed.Load() {
		return
	}
	n := r.announced.Add(1)
	if n > RekeyAnnounceCycles {
		return
	}
	if n == RekeyAnnounceCycles {
		defer r.writeMarker()
	}
	PublishEvent(Event{
		Kind:         EntityDelete,
		Entity:       &Entity{Type: r.legacyType, ID: r.legacyID},
		DeleteReason: ReasonRekeyed,
	})
}

// SameAs returns the alias edge from the new identity to the retired one, or
// ok=false once the announcement window has closed.
//
// The edge carries basis and confidence because the consumer treats a same_as
// WITHOUT a valid confidence as inert — it collapses nothing (ADR 0020). An
// alias missing them travels the whole wire and then does nothing, which is the
// worst of both: the cost of sending it and none of the effect.
func (r *RekeyAnnouncer) SameAs() (Relation, bool) {
	if r == nil {
		return Relation{}, false
	}
	r.checkMarker()
	if r.suppressed.Load() || r.announced.Load() > RekeyAnnounceCycles {
		return Relation{}, false
	}
	return Relation{
		Type:     RelSameAs,
		FromType: r.newType,
		FromID:   r.newID,
		ToType:   r.legacyType,
		ToID:     r.legacyID,
		Attributes: map[string]any{
			"basis":      "rekey",
			"confidence": 1.0,
		},
	}, true
}

// markerKey names the marker of this re-key. A single-key re-key within one
// type keeps the spelling markers were written under before multi-key ids,
// so a host that already announced it does not announce it again.
func (r *RekeyAnnouncer) markerKey() string {
	if r.legacyType == r.newType && len(r.legacyID) == 1 && len(r.newID) == 1 {
		for k, legacy := range r.legacyID {
			if newVal, ok := r.newID[k]; ok {
				return fmt.Sprint(r.legacyType, "|", k, "|", legacy, "|", newVal)
			}
		}
	}
	return identityPayload(r.legacyType, r.legacyID) + "|" + identityPayload(r.newType, r.newID)
}
