// senhub-agent/internal/agent/services/data_store/http_debug.go
package data_store

import (
	"encoding/json"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
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

	response := DebugCacheResponse{
		TotalEntries: len(entries),
		CacheTTL:     d.strategy.cache.ttl.String(),
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
			"ttl":          d.strategy.cache.ttl.String(),
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