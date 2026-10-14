package macindex

import (
	"sync"
	"sync/atomic"
	"time"

	"senhub-agent.go/internal/agent/services/entity"
)

// Entry is one SNMP interface owning a MAC, as known by this agent.
type Entry struct {
	DeviceID string
	Name     string
}

type entry struct {
	deviceID string
	name     string
	seen     time.Time
}

// Index is a concurrency-safe in-process registry of interface MACs seen by
// SNMP polls. It is deliberately small and lazy; expired entries are pruned on
// reads and writes.
type Index struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	now     func() time.Time
	enabled atomic.Bool
}

// New builds an index with the given entry TTL. A TTL <= 0 means entries do
// not expire.
func New(ttl time.Duration) *Index {
	if ttl < 0 {
		ttl = 0
	}
	return &Index{
		entries: make(map[string]entry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// Default is the process-wide shared index. It is constructed explicitly and
// carries no init-time work.
var Default = &Index{
	entries: make(map[string]entry),
	now:     time.Now,
}

// Enabled reports whether resolves_to emission is enabled.
func Enabled() bool { return Default.Enabled() }

// SetEnabled turns the shared index on or off.
func SetEnabled(v bool) { Default.SetEnabled(v) }

// Register adds an interface MAC to the shared index.
func Register(mac, deviceID, name string) { Default.Register(mac, deviceID, name) }

// Lookup returns the shared interface owning mac, if present and fresh.
func Lookup(mac string) (Entry, bool) { return Default.Lookup(mac) }

// SetTTL sets the shared index's TTL.
func SetTTL(d time.Duration) { Default.SetTTL(d) }

// Enabled reports whether this index is enabled.
func (x *Index) Enabled() bool {
	if x == nil {
		return false
	}
	return x.enabled.Load()
}

// SetEnabled sets the enabled flag.
func (x *Index) SetEnabled(v bool) {
	if x == nil {
		return
	}
	x.enabled.Store(v)
}

// SetTTL updates the entry TTL. It is safe to call while Register/Lookup run.
func (x *Index) SetTTL(d time.Duration) {
	if x == nil || d < 0 {
		return
	}
	x.mu.Lock()
	x.ttl = d
	x.mu.Unlock()
}

// Register stores the interface owning mac. Non-canonical MACs are ignored.
func (x *Index) Register(mac, deviceID, name string) {
	if x == nil || deviceID == "" || name == "" {
		return
	}
	canon, ok := entity.CanonicalMACString(mac)
	if !ok {
		return
	}
	now := x.now()
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entries == nil {
		x.entries = make(map[string]entry)
	}
	x.evictLocked(now)
	x.entries[canon] = entry{deviceID: deviceID, name: name, seen: now}
}

// Lookup returns the interface owning mac, if known and within TTL.
func (x *Index) Lookup(mac string) (Entry, bool) {
	if x == nil {
		return Entry{}, false
	}
	canon, ok := entity.CanonicalMACString(mac)
	if !ok {
		return Entry{}, false
	}
	now := x.now()
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entries == nil {
		return Entry{}, false
	}
	x.evictLocked(now)
	e, ok := x.entries[canon]
	if !ok || x.expiredLocked(e.seen, now) {
		if ok {
			delete(x.entries, canon)
		}
		return Entry{}, false
	}
	return Entry{DeviceID: e.deviceID, Name: e.name}, true
}

func (x *Index) evictLocked(now time.Time) {
	for mac, e := range x.entries {
		if x.expiredLocked(e.seen, now) {
			delete(x.entries, mac)
		}
	}
}

func (x *Index) expiredLocked(seen, now time.Time) bool {
	return x.ttl > 0 && !now.Before(seen.Add(x.ttl))
}
