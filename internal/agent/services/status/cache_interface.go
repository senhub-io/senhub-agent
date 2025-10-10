package status

import "time"

// CacheStatisticsProvider defines the interface for getting cache statistics
// This allows the status service to access cache information without tight coupling
type CacheStatisticsProvider interface {
	// GetProbeStatistics returns statistics for each probe
	GetProbeStatistics() map[string]ProbeStatistics

	// GetCacheInfo returns general cache information
	GetCacheInfo() CacheInfo

	// GetTotalEntries returns total number of cache entries
	GetTotalEntries() int

	// GetHealthMetrics returns cache health metrics
	GetHealthMetrics() map[string]interface{}
}

// ProbeStatistics represents statistics for a single probe
type ProbeStatistics struct {
	Name         string    `json:"name"`
	MetricsCount int       `json:"metrics_count"`
	LastUpdate   time.Time `json:"last_update"`
	IsActive     bool      `json:"is_active"`
	LastError    string    `json:"last_error,omitempty"`
}

// CacheInfo represents general cache information
type CacheInfo struct {
	TotalEntries     int       `json:"total_entries"`
	RetentionMinutes int       `json:"retention_minutes"`
	LastCleanup      time.Time `json:"last_cleanup"`
	MemoryUsageMB    float64   `json:"memory_usage_mb"`
}

// Add this field to StatusService struct
// Note: This would be added to the existing StatusService struct
// cacheProvider CacheStatisticsProvider
