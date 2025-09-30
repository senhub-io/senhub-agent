package snmptrap

import (
	"sync"
	"sync/atomic"

	"senhub-agent.go/internal/agent/services/logger"
)

// TrapBuffer is a thread-safe circular buffer for SNMP traps
type TrapBuffer struct {
	buffer   []*EnrichedTrap
	capacity int
	head     int
	tail     int
	count    int
	mutex    sync.Mutex
	logger   *logger.Logger
	
	// Statistics
	stats struct {
		totalAdded   int64
		totalDropped int64
	}
}

// NewTrapBuffer creates a new trap buffer with the specified capacity
func NewTrapBuffer(capacity int, logger *logger.Logger) *TrapBuffer {
	if capacity <= 0 {
		capacity = 1000
	}
	
	return &TrapBuffer{
		buffer:   make([]*EnrichedTrap, capacity),
		capacity: capacity,
		head:     0,
		tail:     0,
		count:    0,
		logger:   logger,
	}
}

// Add adds a trap to the buffer
// Returns false if the buffer is full and the trap was dropped
func (tb *TrapBuffer) Add(trap *EnrichedTrap) bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	// Check if buffer is full
	if tb.count >= tb.capacity {
		atomic.AddInt64(&tb.stats.totalDropped, 1)
		tb.logger.Debug().
			Str("source", trap.SourceHost).
			Str("trap", trap.TrapName).
			Msg("Buffer full, dropping trap")
		return false
	}
	
	// Add trap to buffer
	tb.buffer[tb.tail] = trap
	tb.tail = (tb.tail + 1) % tb.capacity
	tb.count++
	
	atomic.AddInt64(&tb.stats.totalAdded, 1)
	
	tb.logger.Debug().
		Int("buffer_count", tb.count).
		Int("buffer_capacity", tb.capacity).
		Str("trap", trap.TrapName).
		Msg("Trap added to buffer")
	
	return true
}

// Get retrieves up to 'limit' traps from the buffer
// If limit is 0 or negative, retrieves all available traps
func (tb *TrapBuffer) Get(limit int) []*EnrichedTrap {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	if tb.count == 0 {
		return nil
	}
	
	// Determine how many traps to retrieve
	retrieveCount := tb.count
	if limit > 0 && limit < tb.count {
		retrieveCount = limit
	}
	
	// Collect traps
	traps := make([]*EnrichedTrap, 0, retrieveCount)
	for i := 0; i < retrieveCount; i++ {
		trap := tb.buffer[tb.head]
		if trap != nil {
			traps = append(traps, trap)
			tb.buffer[tb.head] = nil // Clear reference
		}
		tb.head = (tb.head + 1) % tb.capacity
	}
	
	tb.count -= retrieveCount
	
	tb.logger.Debug().
		Int("retrieved", retrieveCount).
		Int("remaining", tb.count).
		Msg("Retrieved traps from buffer")
	
	return traps
}

// Flush retrieves all traps from the buffer
func (tb *TrapBuffer) Flush() []*EnrichedTrap {
	return tb.Get(0)
}

// Size returns the current number of traps in the buffer
func (tb *TrapBuffer) Size() int {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	return tb.count
}

// Capacity returns the buffer capacity
func (tb *TrapBuffer) Capacity() int {
	return tb.capacity
}

// GetStats returns buffer statistics
func (tb *TrapBuffer) GetStats() BufferStats {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	return BufferStats{
		CurrentSize:  tb.count,
		Capacity:     tb.capacity,
		TotalAdded:   atomic.LoadInt64(&tb.stats.totalAdded),
		TotalDropped: atomic.LoadInt64(&tb.stats.totalDropped),
	}
}

// Clear removes all traps from the buffer
func (tb *TrapBuffer) Clear() {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	// Clear all references
	for i := 0; i < tb.capacity; i++ {
		tb.buffer[i] = nil
	}
	
	tb.head = 0
	tb.tail = 0
	tb.count = 0
	
	tb.logger.Debug().Msg("Buffer cleared")
}

// IsFull returns true if the buffer is at capacity
func (tb *TrapBuffer) IsFull() bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	return tb.count >= tb.capacity
}

// IsEmpty returns true if the buffer is empty
func (tb *TrapBuffer) IsEmpty() bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	return tb.count == 0
}

// GetUtilization returns the buffer utilization percentage (0-100)
func (tb *TrapBuffer) GetUtilization() float64 {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()
	
	if tb.capacity == 0 {
		return 0
	}
	
	return (float64(tb.count) / float64(tb.capacity)) * 100.0
}