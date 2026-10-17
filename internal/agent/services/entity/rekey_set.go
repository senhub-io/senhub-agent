package entity

import (
	"fmt"
	"sync"
)

// LegacyIdentityFunc derives the legacy spelling of an entity. ok=false means
// this entity was not re-keyed / has no legacy form: it is ignored.
type LegacyIdentityFunc func(e Entity) (legacyType string, legacyID map[string]any, ok bool)

// SetRekeyAnnouncer announces re-key events for a set of entities whose legacy
// identities are derived per entity. Use RekeyAnnouncer for one fixed old/new
// pair; use SetRekeyAnnouncer when a source emits many entities and the mapping
// is computed by a function.
type SetRekeyAnnouncer struct {
	name        string
	derive      LegacyIdentityFunc
	maxEntities int
	warn        func(tracked int)
	warnOnce    sync.Once

	mu                 sync.Mutex
	checked            bool
	suppressed         bool
	closed             bool
	counted            int
	announcedThisCycle bool
	markerWritten      bool

	tracked        map[string]setRekeyRecord
	legacyClaims   map[string]string
	cycleAnnounced map[string]struct{}
	cycleDeleted   map[string]struct{}
}

type setRekeyRecord struct {
	legacyType string
	legacyID   map[string]any
	legacyKey  string
	newType    string
	newID      map[string]any
}

// NewSetRekeyAnnouncer returns an announcer for derived re-keys, or nil when
// derive is nil or maxEntities is not positive.
func NewSetRekeyAnnouncer(name string, derive LegacyIdentityFunc, maxEntities int, warn func(tracked int)) *SetRekeyAnnouncer {
	if derive == nil || maxEntities <= 0 {
		return nil
	}
	return &SetRekeyAnnouncer{
		name:           name,
		derive:         derive,
		maxEntities:    maxEntities,
		warn:           warn,
		tracked:        make(map[string]setRekeyRecord),
		legacyClaims:   make(map[string]string),
		cycleAnnounced: make(map[string]struct{}),
		cycleDeleted:   make(map[string]struct{}),
	}
}

func (s *SetRekeyAnnouncer) checkMarkerLocked() {
	if s.checked {
		return
	}
	present, active := markerPresent("set|" + s.name)
	if !active {
		return
	}
	s.checked = true
	if present {
		s.suppressed = true
	}
}

func (s *SetRekeyAnnouncer) resetCycleLocked() {
	s.announcedThisCycle = false
	s.cycleAnnounced = make(map[string]struct{})
	s.cycleDeleted = make(map[string]struct{})
}

func (s *SetRekeyAnnouncer) writeMarkerLocked() {
	if s.markerWritten {
		return
	}
	s.markerWritten = true
	markerWrite("set|"+s.name, fmt.Sprintf("%s|%d", s.name, len(s.tracked)))
}

// BeginCycle starts a new observation cycle. It lazily checks the marker and
// advances the window for the previous cycle when that cycle announced at
// least one entity.
func (s *SetRekeyAnnouncer) BeginCycle() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkMarkerLocked()
	if s.suppressed {
		s.resetCycleLocked()
		return
	}

	// An empty or all-ignored cycle does not consume the announcement window:
	// a discovery source may take a few cycles before it finds anything.
	if s.announcedThisCycle {
		s.counted++
		if s.counted == RekeyAnnounceCycles {
			s.closed = true
			if len(s.tracked) > 0 {
				s.writeMarkerLocked()
			}
		}
	}
	s.resetCycleLocked()
}

// Close processes the last pending cycle exactly as the next BeginCycle would,
// so a source or test can finish the window without waiting for another cycle.
// It is idempotent.
func (s *SetRekeyAnnouncer) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkMarkerLocked()
	if s.suppressed {
		return
	}
	if s.announcedThisCycle {
		s.counted++
		if s.counted == RekeyAnnounceCycles {
			s.closed = true
			if len(s.tracked) > 0 {
				s.writeMarkerLocked()
			}
		}
		s.resetCycleLocked()
	}
}

// Announce derives the entity's legacy identity and, while the window is open,
// publishes one EntityDelete for the legacy identity and returns the same_as
// alias from the new identity to the legacy one.
func (s *SetRekeyAnnouncer) Announce(e Entity) (Relation, bool) {
	if s == nil {
		return Relation{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkMarkerLocked()
	if s.suppressed || s.closed {
		return Relation{}, false
	}

	legacyType, legacyID, ok := s.derive(e)
	if !ok {
		return Relation{}, false
	}
	if legacyType == e.Type && idMapsEqual(legacyID, e.ID) {
		return Relation{}, false
	}

	entityKey := identityPayload(e.Type, e.ID)
	if _, announced := s.cycleAnnounced[entityKey]; announced {
		return Relation{}, false
	}

	if rec, tracked := s.tracked[entityKey]; tracked {
		return s.announceLocked(rec, entityKey, rec.legacyKey)
	}

	legacyKey := identityPayload(legacyType, legacyID)

	// If two distinct entities derive the same legacy identity, the first one
	// seen claims it. An alias from both entities to one legacy node would
	// merge them; arrival order keeps the result deterministic.
	if claimant, claimed := s.legacyClaims[legacyKey]; claimed && claimant != entityKey {
		return Relation{}, false
	}

	// The tracked maps are the only window-long memory. Dropping a new entity
	// instead of evicting an older one keeps the bound strict and simple.
	if len(s.tracked) >= s.maxEntities {
		s.warnOnce.Do(func() {
			if s.warn != nil {
				s.warn(s.maxEntities)
			}
		})
		return Relation{}, false
	}

	rec := setRekeyRecord{
		legacyType: legacyType,
		legacyID:   cloneID(legacyID),
		legacyKey:  legacyKey,
		newType:    e.Type,
		newID:      cloneID(e.ID),
	}
	s.tracked[entityKey] = rec
	s.legacyClaims[legacyKey] = entityKey
	return s.announceLocked(rec, entityKey, legacyKey)
}

func (s *SetRekeyAnnouncer) announceLocked(rec setRekeyRecord, entityKey, legacyKey string) (Relation, bool) {
	s.announcedThisCycle = true
	s.cycleAnnounced[entityKey] = struct{}{}

	if _, deleted := s.cycleDeleted[legacyKey]; !deleted {
		s.cycleDeleted[legacyKey] = struct{}{}
		PublishEvent(Event{
			Kind:         EntityDelete,
			Entity:       &Entity{Type: rec.legacyType, ID: cloneID(rec.legacyID)},
			DeleteReason: ReasonRekeyed,
		})
	}

	return Relation{
		Type:     RelSameAs,
		FromType: rec.newType,
		FromID:   cloneID(rec.newID),
		ToType:   rec.legacyType,
		ToID:     cloneID(rec.legacyID),
		Attributes: map[string]any{
			"basis":      "rekey",
			"confidence": 1.0,
		},
	}, true
}
