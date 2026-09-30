package status

import "time"

// CacheStatisticsProvider defines the interface for getting cache statistics
// This allows the status service to access cache information without tight coupling
type CacheStatisticsProvider interface {
	// GetProbeStatistics returns statistics for each probe
	GetProbeStatistics() map[string]ProbeStatistics

	// GetTotalEntries returns total number of cache entries
	GetTotalEntries() int
}

// ProbeStatistics represents statistics for a single probe
type ProbeStatistics struct {
	Name         string    `json:"name"`
	MetricsCount int       `json:"metrics_count"`
	LastUpdate   time.Time `json:"last_update"`
	IsActive     bool      `json:"is_active"`
	LastError    string    `json:"last_error,omitempty"`
}
