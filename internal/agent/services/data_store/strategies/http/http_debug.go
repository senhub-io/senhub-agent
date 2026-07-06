// senhub-agent/internal/agent/services/data_store/http_debug.go
package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// DebugManager handles all debug and admin utilities for HTTP endpoints
type DebugManager struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for access to cache and other modules
}

// NewDebugManager creates a new debug and admin utilities manager
func NewDebugManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *DebugManager {
	return &DebugManager{
		logger:   logger,
		strategy: strategy,
	}
}

// Debug Response Types (types for cache debugging are defined in http_cache.go)

// LogLevelInfo represents log level information for debug display
type LogLevelInfo struct {
	Module string `json:"module"`
	Level  string `json:"level"`
}

// LogLevelsResponse represents the debug log levels response
type LogLevelsResponse struct {
	ModuleLevels []LogLevelInfo `json:"module_levels"`
}

// SetLogLevelsRequest represents the request to set log levels
type SetLogLevelsRequest struct {
	ModuleLevels []logger.ModuleLogConfig `json:"module_levels"`
}

// CacheInfoResponse is defined in http_cache.go

// Debug Endpoints

// HandleDebugCache handles GET requests for cache debugging
func (d *DebugManager) HandleDebugCache(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	d.logger.Debug().Msg("Debug cache request received")

	d.strategy.cache.mu.RLock()
	defer d.strategy.cache.mu.RUnlock()

	now := time.Now()
	var entries []DebugCacheEntry
	summary := make(map[string]int)

	// Convert TSDB cache data to debug format
	for probeName, tsKeys := range d.strategy.cache.probeIndex {
		summary[probeName] = len(tsKeys)

		for tsKey := range tsKeys {
			if metric, exists := d.strategy.cache.timeSeries[tsKey]; exists {
				age := now.Sub(metric.Timestamp)

				// Use metric name directly (no probe suffix needed)
				entry := DebugCacheEntry{
					Name:      metric.MetricName,
					Value:     metric.Value,
					Timestamp: metric.Timestamp,
					Unit:      metric.Unit,
					ProbeName: metric.ProbeName,
					Tags:      metric.Tags,
					Age:       age.String(),
				}
				entries = append(entries, entry)
			}
		}
	}

	// Get formatted TTL from cache info
	cacheInfo := d.strategy.cache.GetCacheInfo()

	response := DebugCacheResponse{
		TotalEntries: len(entries),
		CacheTTL:     cacheInfo.TTL,
		Entries:      entries,
		Summary:      summary,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode debug cache response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	d.logger.Debug().
		Int("total_entries", len(entries)).
		Any("summary", summary).
		Msg("Debug cache response sent")
}

// HandleDebugLogs handles GET requests for log level debugging
func (d *DebugManager) HandleDebugLogs(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	d.logger.Debug().Msg("Debug logs request received")

	// Get current module log levels
	moduleLevels := logger.GetModuleLogLevels()

	var logLevels []LogLevelInfo
	for module, level := range moduleLevels {
		logLevels = append(logLevels, LogLevelInfo{
			Module: module,
			Level:  level.String(),
		})
	}

	response := LogLevelsResponse{
		ModuleLevels: logLevels,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode debug logs response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	d.logger.Debug().Int("modules_count", len(logLevels)).Msg("Debug logs response sent")
}

// HandleSetLogLevels handles POST requests to set log levels
func (d *DebugManager) HandleSetLogLevels(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Parse request body
	var req SetLogLevelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.logger.Error().Err(err).Msg("Failed to parse log levels request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	d.logger.Info().
		Int("modules_count", len(req.ModuleLevels)).
		Msg("Setting log levels")

	// Set the log levels
	if err := logger.SetModuleLogLevels(req.ModuleLevels); err != nil {
		d.logger.Error().Err(err).Msg("Failed to set module log levels")
		http.Error(w, "Invalid log configuration", http.StatusBadRequest)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]string{"status": "success", "message": "Log levels updated"}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode set logs response")
		return
	}

	d.logger.Info().Msg("Log levels updated successfully")
}

// Admin Endpoints

// HandleStatsCache handles GET requests for cache statistics
func (d *DebugManager) HandleStatsCache(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Get cache statistics
	stats := d.strategy.cache.GetStatistics()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode cache stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// HandleConfigProbes handles GET requests for probe configuration
func (d *DebugManager) HandleConfigProbes(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// For now, return active probes from cache since we don't have direct access to configuration provider
	// TODO: Refactor to pass configuration provider to HTTP strategy for full probe config access
	d.strategy.cache.mu.RLock()
	activeProbes := make(map[string]bool)
	for _, metric := range d.strategy.cache.timeSeries {
		activeProbes[metric.ProbeName] = true
	}
	d.strategy.cache.mu.RUnlock()

	// Create simplified probe list
	probes := make([]map[string]interface{}, 0)
	for probeName := range activeProbes {
		probes = append(probes, map[string]interface{}{
			"name":    probeName,
			"type":    "detected",
			"enabled": true,
			"status":  "active",
		})
	}

	response := map[string]interface{}{
		"probes": probes,
		"count":  len(probes),
		"note":   "Showing active probes from cache. Full configuration requires restart to change.",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode probe config")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// HandleAdminCacheClear handles POST requests to clear the cache
func (d *DebugManager) HandleAdminCacheClear(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Clear the cache
	d.strategy.cache.Clear()

	d.logger.Info().Msg("Cache cleared via admin API")

	response := map[string]string{
		"status":  "success",
		"message": "Cache cleared successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode cache clear response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// HandleTestInjectMetrics handles POST requests to inject test redfish metrics (TEMPORARY TEST ENDPOINT)
func (d *DebugManager) HandleTestInjectMetrics(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	d.logger.Info().Msg("Injecting test redfish metrics for validation")

	now := time.Now()

	// Create realistic Dell PowerVault ME storage metrics
	testMetrics := []datapoint.DataPoint{
		// Pool metrics with manufacturer/model tags for Dell corrections
		{
			Name:      "storage.pool.capacity.total",
			Value:     float64(10995116277760), // 10TB in bytes - should become ~10.0 TB
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "endpoint", Value: "https://powervault.example.com"},
				{Key: "controller", Value: "controller_a"},
				{Key: "pool_name", Value: "Pool A"}, // This should appear in filters!
				{Key: "pool_id", Value: "A"},
				{Key: "manufacturer", Value: "Dell"}, // Enables Dell corrections
				{Key: "model", Value: "PowerVault"},  // Enables Dell corrections
			},
		},
		{
			Name:      "storage.pool.capacity.available",
			Value:     float64(8796093022208), // 8TB available in bytes - should become ~8.0 TB
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "endpoint", Value: "https://powervault.example.com"},
				{Key: "controller", Value: "controller_a"},
				{Key: "pool_name", Value: "Pool A"},
				{Key: "pool_id", Value: "A"},
				{Key: "manufacturer", Value: "Dell"},
				{Key: "model", Value: "PowerVault"},
			},
		},
		// Volume metrics
		{
			Name:      "storage.volume.capacity.total",
			Value:     float64(5497558138880), // 5TB volume in bytes
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "endpoint", Value: "https://powervault.example.com"},
				{Key: "controller", Value: "controller_a"},
				{Key: "pool_name", Value: "Pool A"},      // Should appear in filters
				{Key: "volume_name", Value: "Volume001"}, // Should appear in filters
				{Key: "volume_id", Value: "00c0fffd07cd00005a728a6701000000"},
				{Key: "volume_type", Value: "standard"},
				{Key: "raid_type", Value: "RAID5"}, // Should appear in filters
				{Key: "manufacturer", Value: "Dell"},
				{Key: "model", Value: "PowerVault"},
			},
		},
		// Drive metrics - with drive_id that should be HIDDEN
		{
			Name:      "storage.drive.capacity.total",
			Value:     float64(2000398934016), // 2TB drive in bytes
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "endpoint", Value: "https://powervault.example.com"},
				{Key: "controller", Value: "controller_a"},
				{Key: "drive_name", Value: "Drive 3"}, // Should appear in filters
				{Key: "drive_id", Value: "0.3"},       // Should be HIDDEN in filters
				{Key: "drive_type", Value: "HDD"},
				{Key: "manufacturer", Value: "Dell"},
				{Key: "model", Value: "PowerVault"},
			},
		},
		// Multiple pools to test filtering
		{
			Name:      "storage.pool.capacity.total",
			Value:     float64(5497558138880), // 5TB pool
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "endpoint", Value: "https://powervault.example.com"},
				{Key: "controller", Value: "controller_b"},
				{Key: "pool_name", Value: "dgA01"}, // Different pool name
				{Key: "pool_id", Value: "dgA01"},
				{Key: "manufacturer", Value: "Dell"},
				{Key: "model", Value: "PowerVault"},
			},
		},
	}

	// Inject metrics into cache using transformer registry
	d.strategy.cache.AddDataPointsWithTransformer(testMetrics, d.strategy.transformerRegistry)

	d.logger.Info().
		Int("metrics_injected", len(testMetrics)).
		Msg("Test redfish metrics injected successfully")

	response := map[string]interface{}{
		"status":        "success",
		"message":       "Test redfish metrics injected successfully",
		"metrics_count": len(testMetrics),
		"instructions":  "Now check Tag Filters - pool_name should appear, drive_id should be hidden",
		"test_urls": map[string]string{
			"dashboard":    "http://localhost:8080/web/2a5d71c5-706e-43ce-8a10-8ee252f85772/dashboard",
			"tag_filters":  "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/info/tags/redfish",
			"prtg_metrics": "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/prtg/metrics/redfish",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode test inject response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// RealMetricInjectionRequest represents the request structure for real metric injection
type RealMetricInjectionRequest struct {
	Metrics []struct {
		Name      string            `json:"name"`
		Value     interface{}       `json:"value"`
		Unit      string            `json:"unit"`
		Timestamp string            `json:"timestamp"`
		Tags      map[string]string `json:"tags"`
	} `json:"metrics"`
	Source string `json:"source"`
}

// HandleInjectRealMetrics handles POST requests to inject real production metrics
func (d *DebugManager) HandleInjectRealMetrics(w http.ResponseWriter, r *http.Request) {
	_, authenticated := d.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	d.logger.Info().Msg("Injecting real production metrics for testing")

	// Parse request body
	var request RealMetricInjectionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		d.logger.Error().Err(err).Msg("Failed to decode real metrics injection request")
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Convert to datapoint format
	var dataPoints []datapoint.DataPoint
	now := time.Now()

	for _, metric := range request.Metrics {
		// Parse timestamp
		var timestamp time.Time
		if parsedTime, err := time.Parse(time.RFC3339, metric.Timestamp); err == nil {
			timestamp = parsedTime
		} else {
			timestamp = now // Fallback to current time
		}

		// Convert tags map to Tag slice
		var tagSlice []tags.Tag
		for key, value := range metric.Tags {
			tagSlice = append(tagSlice, tags.Tag{Key: key, Value: value})
		}

		// Convert value to float64 (DataPoint.Value type)
		var value float64
		switch v := metric.Value.(type) {
		case float64:
			value = v
		case float32:
			value = float64(v)
		case int:
			value = float64(v)
		case int64:
			value = float64(v)
		case int32:
			value = float64(v)
		default:
			// Try to convert via float64 if possible
			if f, ok := v.(float64); ok {
				value = f
			} else {
				value = 0 // Default fallback
			}
		}

		dataPoint := datapoint.DataPoint{
			Name:      metric.Name,
			Value:     value,
			Timestamp: timestamp,
			Tags:      tagSlice,
		}

		dataPoints = append(dataPoints, dataPoint)
	}

	// Inject metrics into cache using transformer registry
	d.strategy.cache.AddDataPointsWithTransformer(dataPoints, d.strategy.transformerRegistry)

	d.logger.Info().
		Int("metrics_injected", len(dataPoints)).
		Str("source", request.Source).
		Msg("Real production metrics injected successfully")

	// Count metrics by type for better reporting
	metricsByType := make(map[string]int)
	for _, dp := range dataPoints {
		if dp.Name != "" {
			if len(dp.Name) >= 8 && dp.Name[:8] == "hardware" {
				parts := strings.Split(dp.Name, ".")
				if len(parts) >= 3 {
					metricType := strings.Join(parts[:3], ".")
					metricsByType[metricType]++
				}
			} else {
				// Handle other metric patterns
				parts := strings.Split(dp.Name, ".")
				if len(parts) >= 2 {
					metricType := strings.Join(parts[:2], ".")
					metricsByType[metricType]++
				}
			}
		}
	}

	response := map[string]interface{}{
		"status":          "success",
		"message":         "Real production metrics injected successfully",
		"metrics_count":   len(dataPoints),
		"source":          request.Source,
		"metrics_by_type": metricsByType,
		"instructions":    "Test contextual filtering with real production data",
		"test_urls": map[string]string{
			"dashboard":    "http://localhost:8080/web/2a5d71c5-706e-43ce-8a10-8ee252f85772/dashboard",
			"tag_filters":  "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/info/tags/redfish",
			"prtg_metrics": "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/prtg/metrics/redfish",
		},
		"contextual_filtering_examples": map[string]string{
			"pool_metrics":   "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/prtg/metrics/redfish?tags=pool_name:A",
			"volume_metrics": "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/prtg/metrics/redfish?tags=volume_name:Volume001",
			"drive_metrics":  "http://localhost:8080/api/2a5d71c5-706e-43ce-8a10-8ee252f85772/prtg/metrics/redfish?tags=drive_name:Drive%200.0",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		d.logger.Error().Err(err).Msg("Failed to encode real metrics inject response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// Utility Methods

// GetDebugInfo returns comprehensive debug information
func (d *DebugManager) GetDebugInfo() map[string]interface{} {
	d.strategy.cache.mu.RLock()
	totalMetrics := len(d.strategy.cache.timeSeries)
	probeCount := len(d.strategy.cache.probeIndex)
	d.strategy.cache.mu.RUnlock()

	return map[string]interface{}{
		"cache": map[string]interface{}{
			"total_metrics": totalMetrics,
			"active_probes": probeCount,
			"ttl":           d.strategy.cache.ttl.String(),
		},
		"server": d.strategy.serverManager.GetServerStats(),
		"config": d.strategy.configManager.GetConfigurationSummary(),
		"health": d.strategy.healthManager.GetHealthMetrics(),
	}
}

// GetSystemDiagnostics returns system diagnostic information
func (d *DebugManager) GetSystemDiagnostics() map[string]interface{} {
	return map[string]interface{}{
		"modules": map[string]string{
			"authentication": "active",
			"health":         "active",
			"metrics":        "active",
			"configuration":  "active",
			"server":         "active",
			"cache":          "active",
			"debug":          "active",
		},
		"endpoints_enabled": d.strategy.configManager.GetEnabledEndpointsList(),
		"server_running":    d.strategy.serverManager.IsRunning(),
		"cache_healthy":     d.strategy.cache != nil,
	}
}
