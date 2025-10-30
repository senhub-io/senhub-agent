// senhub-agent/internal/agent/services/data_store/http_cache.go
package http

import (
	"fmt"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// DiscriminantTagsRegistry defines which tags are discriminant (identify unique instances)
// vs contextual (provide metadata) for each probe type.
//
// Discriminant tags MUST be included in the time series key because metrics with different
// discriminant tag values represent DIFFERENT physical/logical instances that can have
// different metric values at the same timestamp.
//
// Contextual tags should NOT be included in the key - they provide additional metadata
// but don't identify distinct metric sources. Including them would break time series
// continuity when metadata changes (e.g., endpoint URL change, platform info update).
//
// Based on TIME_SERIES_KEY_DESIGN.md - Universal Uniqueness Rule (RUU):
// "Une clé de série temporelle DOIT être unique SI ET SEULEMENT SI
// les valeurs des métriques collectées à cet instant peuvent être DIFFÉRENTES"
var DiscriminantTagsRegistry = map[string][]string{
	// System probes - multi-instance metrics
	"cpu": {"core"},                               // Different CPU cores have independent values
	"memory": {},                                  // System-level only, no instances
	"network": {"interface", "adapter"},           // Different network interfaces
	"logicaldisk": {"drive", "mount_point", "device"}, // Different drives/volumes

	// Application probes
	"citrix": {"metric_type", "failure_category"}, // Citrix aggregation types
	"webapp": {"url", "endpoint"},                 // Different web endpoints
	"gateway": {"destination", "target"},          // Different gateway targets

	// Infrastructure probes
	"redfish": {
		// Storage components
		"controller", "controller_id",
		"drive_id", "drive_name",
		"volume_id", "volume_name",
		"pool_name", "pool_id",
		// Other hardware components
		"psu_name", "psu_id",
		"processor_id",
		"memory_module_id",
		"fan_name", "sensor_name",
	},

	// Event probes
	"winevents": {"event_id", "source"}, // Windows Event Log events
	"syslog": {"event_id", "source"},    // Syslog events

	// OpenTelemetry
	"otel": {"service_name", "span_name"}, // OTEL traces/metrics
}

// MetricCache stores the latest metrics in memory with TTL, organized like a TSDB
type MetricCache struct {
	mu sync.RWMutex
	// TSDB-like structure: unique key -> latest metric
	// Key format: probe_name:metric_name:discriminant_tags
	// Example: "cpu:usage_percent:core=0" or "redfish:storage.drive.temperature:drive_id=disk.bay.0"
	// Only discriminant tags are in the key - contextual tags are in CachedMetric.Tags
	timeSeries map[string]CachedMetric
	// Index by probe for fast probe-specific queries
	probeIndex map[string]map[string]bool // probe_name -> set of ts_keys
	ttl        time.Duration
	stopChan   chan struct{}
	logger     *logger.ModuleLogger
}

// CachedMetric represents a stored metric with metadata
type CachedMetric struct {
	Value      interface{}
	Timestamp  time.Time
	Unit       string
	ProbeName  string
	MetricName string
	Tags       map[string]string
}

// NewMetricCache creates a new metric cache with the specified TTL
func NewMetricCache(ttl time.Duration, logger *logger.ModuleLogger) *MetricCache {
	return &MetricCache{
		timeSeries: make(map[string]CachedMetric),
		probeIndex: make(map[string]map[string]bool),
		ttl:        ttl,
		stopChan:   make(chan struct{}),
		logger:     logger,
	}
}

// generateTimeSeriesKey creates a unique key for a time series based on probe, metric name,
// and ONLY discriminant tags (tags that identify unique metric instances).
//
// This implements the Universal Uniqueness Rule (RUU) from TIME_SERIES_KEY_DESIGN.md:
// Keys are unique IF AND ONLY IF the metric values can be different.
//
// Example:
//   - CPU probe with core=0 and core=1 → different keys (different CPU cores)
//   - Same CPU core with different "platform" tags → SAME key (contextual metadata)
//   - Redfish with different drive_id → different keys (different physical drives)
//   - Same drive with different "endpoint" URL → SAME key (same physical drive, DNS changed)
//
// This ensures:
//   - Time series continuity when metadata changes (endpoint, hostname, etc.)
//   - Proper cardinality (only multi-instance metrics create multiple series)
//   - Filtering still works (all tags preserved in CachedMetric.Tags)
func (c *MetricCache) generateTimeSeriesKey(probeName, metricName string, tags map[string]string) string {
	// Get discriminant tags for this probe type from registry
	discriminantTagNames, exists := DiscriminantTagsRegistry[probeName]
	if !exists {
		// Unknown probe type - log warning and use no discriminant tags
		// This is safe: creates single time series per metric (like system-level probes)
		c.logger.Warn().
			Str("probe_name", probeName).
			Str("metric_name", metricName).
			Msg("⚠️ Probe type not in DiscriminantTagsRegistry - using no discriminant tags")
		discriminantTagNames = []string{}
	}

	// Extract only discriminant tag values that are present
	var tagParts []string
	discriminantTagKeys := make([]string, 0, len(discriminantTagNames))

	for _, tagName := range discriminantTagNames {
		if _, exists := tags[tagName]; exists {
			discriminantTagKeys = append(discriminantTagKeys, tagName)
		}
	}

	// Sort discriminant tag keys for consistent key generation
	for i := 0; i < len(discriminantTagKeys); i++ {
		for j := i + 1; j < len(discriminantTagKeys); j++ {
			if discriminantTagKeys[i] > discriminantTagKeys[j] {
				discriminantTagKeys[i], discriminantTagKeys[j] = discriminantTagKeys[j], discriminantTagKeys[i]
			}
		}
	}

	// Build tag string from discriminant tags only
	for _, k := range discriminantTagKeys {
		tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, tags[k]))
	}

	// Create unique key: probe:metric:discriminant_tags
	if len(tagParts) > 0 {
		return fmt.Sprintf("%s:%s:%s", probeName, metricName, joinStrings(tagParts, ","))
	}
	return fmt.Sprintf("%s:%s", probeName, metricName)
}

// AddDataPointsWithTransformer adds data points to the cache using external transformer
func (c *MetricCache) AddDataPointsWithTransformer(dataPoints []datapoint.DataPoint, transformerRegistry *transformers.TransformerRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Debug().
		Int("data_points", len(dataPoints)).
		Msg("💾 Cache - Adding data points")

	now := time.Now()

	for _, dp := range dataPoints {
		// Convert tags from []tags.Tag to map[string]string
		tags := make(map[string]string)
		for _, tag := range dp.Tags {
			tags[tag.Key] = tag.Value
		}

		// Get probe name from tags
		probeName := tags["probe_name"]

		// ⚠️ DEBUG: Log if probe_name is missing or empty
		if probeName == "" {
			c.logger.Warn().
				Str("metric_name", dp.Name).
				Interface("all_tags", tags).
				Msg("⚠️ MISSING PROBE_NAME: Metric has no probe_name tag!")
			probeName = "unknown" // Fallback for metrics without probe_name
		}

		// Generate unique time series key
		tsKey := c.generateTimeSeriesKey(probeName, dp.Name, tags)

		// Get transformer to resolve unit
		transformer, err := transformerRegistry.LoadTransformer(probeName, "friendly")
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("probe_name", probeName).
				Msg("Failed to get transformer for unit resolution")
		}

		// Resolve unit using transformer
		unit := ""
		if transformer != nil {
			unit = transformer.GetUnit(dp.Name)
		}

		// Note: Unit corrections are now applied earlier in the data processing pipeline (data_store.go)
		// before routing to strategies, so datapoints here already have corrected values

		// Store the metric (value already corrected by data_store.go)
		cachedMetric := CachedMetric{
			Value:      dp.Value,
			Timestamp:  now, // Use consistent timestamp for write batch
			Unit:       unit,
			ProbeName:  probeName,
			MetricName: dp.Name,
			Tags:       tags,
		}

		// TSDB approach: Store/replace metric by unique key (deduplication at write-time)
		existingMetric, exists := c.timeSeries[tsKey]
		if exists {
			c.logger.Debug().
				Str("ts_key", tsKey).
				Time("old_timestamp", existingMetric.Timestamp).
				Time("new_timestamp", cachedMetric.Timestamp).
				Msg("🔄 Replacing existing metric in time series")
		} else {
			c.logger.Debug().
				Str("ts_key", tsKey).
				Str("metric_name", dp.Name).
				Str("probe_name", probeName).
				Msg("📊 Adding new metric to time series")
		}

		c.timeSeries[tsKey] = cachedMetric

		// Update probe index
		if c.probeIndex[probeName] == nil {
			c.probeIndex[probeName] = make(map[string]bool)
		}
		c.probeIndex[probeName][tsKey] = true

		c.logger.Debug().
			Str("ts_key", tsKey).
			Str("probe", probeName).
			Str("metric", dp.Name).
			Interface("value", dp.Value).
			Str("unit", unit).
			Msg("💾 Cache - Stored metric")
	}
}

// cleanup removes expired metrics from the cache
func (c *MetricCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	// Find expired metrics
	for key, metric := range c.timeSeries {
		if now.Sub(metric.Timestamp) > c.ttl {
			expiredKeys = append(expiredKeys, key)
		}
	}

	// Remove expired metrics
	for _, key := range expiredKeys {
		metric := c.timeSeries[key]
		delete(c.timeSeries, key)

		// Update probe index
		if probeKeys, exists := c.probeIndex[metric.ProbeName]; exists {
			delete(probeKeys, key)
			if len(probeKeys) == 0 {
				delete(c.probeIndex, metric.ProbeName)
			}
		}
	}

	if len(expiredKeys) > 0 {
		c.logger.Debug().
			Int("expired_count", len(expiredKeys)).
			Msg("💾 Cache - Cleaned up expired metrics")
	}
}

// GetAllMetrics returns all metrics currently in the cache
func (c *MetricCache) GetAllMetrics() []CachedMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make([]CachedMetric, 0, len(c.timeSeries))
	for _, metric := range c.timeSeries {
		metrics = append(metrics, metric)
	}

	return metrics
}

// GetProbeMetrics returns all metrics for a specific probe
func (c *MetricCache) GetProbeMetrics(probeName string) []CachedMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := make([]CachedMetric, 0)

	if keys, exists := c.probeIndex[probeName]; exists {
		for key := range keys {
			if metric, exists := c.timeSeries[key]; exists {
				metrics = append(metrics, metric)
			}
		}
	}

	return metrics
}

// GetCacheInfo returns cache statistics
func (c *MetricCache) GetCacheInfo() CacheInfoResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheInfoResponse{
		TotalMetrics: len(c.timeSeries),
		ProbeCount:   len(c.probeIndex),
		TTL:          formatTTL(c.ttl),
	}
}

// formatTTL formats a duration into a human-readable string
func formatTTL(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d hour%s %d minute%s", hours, pluralize(hours), minutes, pluralize(minutes))
		}
		return fmt.Sprintf("%d hour%s", hours, pluralize(hours))
	}

	if minutes > 0 {
		if seconds > 0 && minutes < 5 { // Show seconds for short durations
			return fmt.Sprintf("%d minute%s %d second%s", minutes, pluralize(minutes), seconds, pluralize(seconds))
		}
		return fmt.Sprintf("%d minute%s", minutes, pluralize(minutes))
	}

	return fmt.Sprintf("%d second%s", seconds, pluralize(seconds))
}

// pluralize returns "s" if count != 1, otherwise empty string
func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// ProbeStatistics represents statistics for a single probe
type ProbeStatistics struct {
	Name         string    `json:"name"`
	MetricsCount int       `json:"metrics_count"`
	LastUpdate   time.Time `json:"last_update"`
}

// GetProbeStatistics returns statistics for each probe
func (c *MetricCache) GetProbeStatistics() map[string]ProbeStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	probeStats := make(map[string]ProbeStatistics)

	for probeName, tsKeys := range c.probeIndex {
		if probeName == "" {
			probeName = "unknown"
		}

		metricCount := len(tsKeys)
		var lastUpdate time.Time

		// Track latest update time for each probe
		for tsKey := range tsKeys {
			if metric, exists := c.timeSeries[tsKey]; exists {
				if lastUpdate.IsZero() || metric.Timestamp.After(lastUpdate) {
					lastUpdate = metric.Timestamp
				}
			}
		}

		probeStats[probeName] = ProbeStatistics{
			Name:         probeName,
			MetricsCount: metricCount,
			LastUpdate:   lastUpdate,
		}
	}

	return probeStats
}

// GetDebugInfo returns detailed cache information for debugging
func (c *MetricCache) GetDebugInfo() DebugCacheResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]DebugCacheEntry, 0, len(c.timeSeries))
	summary := make(map[string]int)

	for key, metric := range c.timeSeries {
		entries = append(entries, DebugCacheEntry{
			Key:       key,
			Name:      metric.MetricName,
			Value:     metric.Value,
			Timestamp: metric.Timestamp,
			Unit:      metric.Unit,
			ProbeName: metric.ProbeName,
			Tags:      metric.Tags,
			Age:       time.Since(metric.Timestamp).String(),
		})

		// Build summary by probe
		summary[metric.ProbeName]++
	}

	return DebugCacheResponse{
		TotalEntries: len(c.timeSeries),
		CacheTTL:     c.ttl.String(),
		Entries:      entries,
		Summary:      summary,
	}
}

// StartCleanupRoutine starts the background cleanup goroutine
func (c *MetricCache) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(c.ttl / 2) // Cleanup every half TTL
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.cleanup()
			case <-c.stopChan:
				return
			}
		}
	}()
}

// Stop stops the cache cleanup routine
func (c *MetricCache) Stop() {
	close(c.stopChan)
}

// UpdateTTL updates the cache TTL dynamically
func (c *MetricCache) UpdateTTL(newTTL time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	oldTTL := c.ttl
	c.ttl = newTTL

	c.logger.Info().
		Dur("old_ttl", oldTTL).
		Dur("new_ttl", newTTL).
		Msg("🔄 Cache TTL updated dynamically")
}

// CacheInfoResponse represents cache statistics
type CacheInfoResponse struct {
	TotalMetrics int    `json:"total_metrics"`
	ProbeCount   int    `json:"probe_count"`
	TTL          string `json:"ttl"`
	MemoryUsage  string `json:"memory_usage"`
}

// DebugCacheEntry represents a single cache entry for debugging
type DebugCacheEntry struct {
	Key       string            `json:"key"`
	Name      string            `json:"name"`
	Value     interface{}       `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit"`
	ProbeName string            `json:"probe_name"`
	Tags      map[string]string `json:"tags"`
	Age       string            `json:"age"`
}

// DebugCacheResponse represents the complete cache state for debugging
type DebugCacheResponse struct {
	TotalEntries int               `json:"total_entries"`
	CacheTTL     string            `json:"cache_ttl"`
	Entries      []DebugCacheEntry `json:"entries"`
	Summary      map[string]int    `json:"summary"`
}

// Helper function to join strings (since we removed dependency on strings.Join)
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// GetStatistics returns cache statistics for administration
func (c *MetricCache) GetStatistics() CacheStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStatistics{
		TotalMetrics: len(c.timeSeries),
		Probes:       make([]ProbeStatistics, 0),
	}

	// Count metrics per probe
	probeMetrics := make(map[string]int)
	lastUpdated := make(map[string]time.Time)

	for _, metric := range c.timeSeries {
		probeMetrics[metric.ProbeName]++
		if existing, ok := lastUpdated[metric.ProbeName]; !ok || metric.Timestamp.After(existing) {
			lastUpdated[metric.ProbeName] = metric.Timestamp
		}
	}

	// Create probe statistics
	for probeName, count := range probeMetrics {
		stats.Probes = append(stats.Probes, ProbeStatistics{
			Name:         probeName,
			MetricsCount: count,
			LastUpdate:   lastUpdated[probeName],
		})
	}

	return stats
}

// Clear removes all cached metrics
func (c *MetricCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info().
		Int("cleared_metrics", len(c.timeSeries)).
		Msg("Clearing all cached metrics")

	// Clear all data structures
	c.timeSeries = make(map[string]CachedMetric)
	c.probeIndex = make(map[string]map[string]bool)
}

// CacheStatistics represents cache statistics for administration
type CacheStatistics struct {
	TotalMetrics int               `json:"total_metrics"`
	Probes       []ProbeStatistics `json:"probes"`
}
