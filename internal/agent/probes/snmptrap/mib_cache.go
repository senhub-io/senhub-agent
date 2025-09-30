package snmptrap

import (
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// MIBCache provides LRU caching for OID resolutions
type MIBCache struct {
	entries   map[string]*cacheEntry
	order     *cacheNode
	capacity  int
	size      int
	mutex     sync.RWMutex
	logger    *logger.Logger
	ttl       time.Duration
	
	// Statistics
	hits   int64
	misses int64
}

// cacheEntry represents a cached OID resolution
type cacheEntry struct {
	resolved  *ResolvedOID
	node      *cacheNode
	createdAt time.Time
	accessedAt time.Time
}

// cacheNode is a node in the LRU doubly-linked list
type cacheNode struct {
	key  string
	prev *cacheNode
	next *cacheNode
}

// NewMIBCache creates a new MIB cache
func NewMIBCache(capacity int, logger *logger.Logger) *MIBCache {
	if capacity <= 0 {
		capacity = 10000
	}
	
	// Create sentinel node for LRU list
	sentinel := &cacheNode{}
	sentinel.prev = sentinel
	sentinel.next = sentinel
	
	return &MIBCache{
		entries:  make(map[string]*cacheEntry),
		order:    sentinel,
		capacity: capacity,
		size:     0,
		logger:   logger,
		ttl:      24 * time.Hour, // Default 24h TTL
	}
}

// Get retrieves a cached OID resolution
func (mc *MIBCache) Get(oid string) *ResolvedOID {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	entry, exists := mc.entries[oid]
	if !exists {
		mc.misses++
		return nil
	}
	
	// Check TTL
	if time.Since(entry.createdAt) > mc.ttl {
		mc.removeEntry(oid, entry)
		mc.misses++
		return nil
	}
	
	// Move to front (most recently used)
	mc.moveToFront(entry.node)
	entry.accessedAt = time.Now()
	
	mc.hits++
	return entry.resolved
}

// Set stores an OID resolution in the cache
func (mc *MIBCache) Set(oid string, resolved *ResolvedOID) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	// Check if entry already exists
	if entry, exists := mc.entries[oid]; exists {
		entry.resolved = resolved
		entry.accessedAt = time.Now()
		mc.moveToFront(entry.node)
		return
	}
	
	// Create new entry
	now := time.Now()
	node := &cacheNode{key: oid}
	entry := &cacheEntry{
		resolved:   resolved,
		node:       node,
		createdAt:  now,
		accessedAt: now,
	}
	
	// Add to cache
	mc.entries[oid] = entry
	mc.addToFront(node)
	mc.size++
	
	// Evict if over capacity
	if mc.size > mc.capacity {
		mc.evictLRU()
	}
}

// addToFront adds a node to the front of the LRU list
func (mc *MIBCache) addToFront(node *cacheNode) {
	node.prev = mc.order
	node.next = mc.order.next
	mc.order.next.prev = node
	mc.order.next = node
}

// removeNode removes a node from the LRU list
func (mc *MIBCache) removeNode(node *cacheNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// moveToFront moves a node to the front of the LRU list
func (mc *MIBCache) moveToFront(node *cacheNode) {
	mc.removeNode(node)
	mc.addToFront(node)
}

// evictLRU removes the least recently used entry
func (mc *MIBCache) evictLRU() {
	if mc.size == 0 {
		return
	}
	
	// Get LRU node (last in the list)
	lru := mc.order.prev
	if lru == mc.order {
		return // Empty list
	}
	
	// Remove from cache
	if entry, exists := mc.entries[lru.key]; exists {
		mc.removeEntry(lru.key, entry)
	}
}

// removeEntry removes an entry from the cache
func (mc *MIBCache) removeEntry(oid string, entry *cacheEntry) {
	delete(mc.entries, oid)
	mc.removeNode(entry.node)
	mc.size--
}

// Size returns the current cache size
func (mc *MIBCache) Size() int {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	return mc.size
}

// Clean removes expired entries from the cache
func (mc *MIBCache) Clean() {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	now := time.Now()
	expiredKeys := make([]string, 0)
	
	// Find expired entries
	for oid, entry := range mc.entries {
		if now.Sub(entry.createdAt) > mc.ttl {
			expiredKeys = append(expiredKeys, oid)
		}
	}
	
	// Remove expired entries
	for _, oid := range expiredKeys {
		if entry, exists := mc.entries[oid]; exists {
			mc.removeEntry(oid, entry)
		}
	}
	
	if len(expiredKeys) > 0 {
		mc.logger.Debug().
			Int("expired_count", len(expiredKeys)).
			Int("cache_size", mc.size).
			Msg("Cleaned expired MIB cache entries")
	}
}

// Clear removes all entries from the cache
func (mc *MIBCache) Clear() {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	mc.entries = make(map[string]*cacheEntry)
	mc.order.next = mc.order
	mc.order.prev = mc.order
	mc.size = 0
	mc.hits = 0
	mc.misses = 0
	
	mc.logger.Debug().Msg("MIB cache cleared")
}

// GetStats returns cache statistics
func (mc *MIBCache) GetStats() map[string]interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	
	hitRate := float64(0)
	total := mc.hits + mc.misses
	if total > 0 {
		hitRate = float64(mc.hits) / float64(total) * 100
	}
	
	utilization := float64(0)
	if mc.capacity > 0 {
		utilization = float64(mc.size) / float64(mc.capacity) * 100
	}
	
	return map[string]interface{}{
		"size":         mc.size,
		"capacity":     mc.capacity,
		"hits":         mc.hits,
		"misses":       mc.misses,
		"hit_rate":     hitRate,
		"utilization":  utilization,
	}
}