package snmptrap

import (
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// RateLimiter implements rate limiting for SNMP traps
type RateLimiter struct {
	maxPerMinute int
	perSourceMax int
	sources      map[string]*sourceRateInfo
	mutex        sync.RWMutex
	logger       *logger.Logger
}

// sourceRateInfo tracks rate limit info for a source
type sourceRateInfo struct {
	count      int
	windowStart time.Time
	lastSeen   time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxPerMinute, perSourceMax int, logger *logger.Logger) *RateLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 200
	}
	if perSourceMax <= 0 {
		perSourceMax = 50
	}
	
	return &RateLimiter{
		maxPerMinute: maxPerMinute,
		perSourceMax: perSourceMax,
		sources:      make(map[string]*sourceRateInfo),
		logger:       logger,
	}
}

// Allow checks if a trap from the source should be allowed
func (rl *RateLimiter) Allow(sourceIP string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	now := time.Now()
	
	// Get or create source info
	info, exists := rl.sources[sourceIP]
	if !exists {
		info = &sourceRateInfo{
			count:       0,
			windowStart: now,
			lastSeen:    now,
		}
		rl.sources[sourceIP] = info
	}
	
	// Check if we need to reset the window (1-minute window)
	if now.Sub(info.windowStart) >= time.Minute {
		info.count = 0
		info.windowStart = now
	}
	
	// Check rate limit
	if info.count >= rl.perSourceMax {
		rl.logger.Debug().
			Str("source", sourceIP).
			Int("count", info.count).
			Int("limit", rl.perSourceMax).
			Msg("Source rate limit exceeded")
		return false
	}
	
	// Update counters
	info.count++
	info.lastSeen = now
	
	return true
}

// Cleanup removes stale entries from the rate limiter
func (rl *RateLimiter) Cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	now := time.Now()
	staleThreshold := 5 * time.Minute
	
	// Remove entries that haven't been seen recently
	for ip, info := range rl.sources {
		if now.Sub(info.lastSeen) > staleThreshold {
			delete(rl.sources, ip)
			rl.logger.Debug().
				Str("source", ip).
				Msg("Removed stale rate limiter entry")
		}
	}
	
	rl.logger.Debug().
		Int("active_sources", len(rl.sources)).
		Msg("Rate limiter cleanup completed")
}

// GetStats returns rate limiter statistics
func (rl *RateLimiter) GetStats() map[string]interface{} {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()
	
	activeCount := 0
	throttledCount := 0
	
	for _, info := range rl.sources {
		if time.Since(info.windowStart) < time.Minute {
			activeCount++
			if info.count >= rl.perSourceMax {
				throttledCount++
			}
		}
	}
	
	return map[string]interface{}{
		"active_sources":    activeCount,
		"throttled_sources": throttledCount,
		"total_sources":     len(rl.sources),
		"max_per_minute":    rl.maxPerMinute,
		"per_source_max":    rl.perSourceMax,
	}
}

// Reset clears all rate limiter state
func (rl *RateLimiter) Reset() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	rl.sources = make(map[string]*sourceRateInfo)
	rl.logger.Debug().Msg("Rate limiter reset")
}