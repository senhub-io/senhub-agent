// senhub-agent/internal/agent/services/data_store/http_utils.go
package data_store

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// CPUTracker holds CPU measurement state for calculating usage
type CPUTracker struct {
	lastCPUTime    time.Duration
	lastWallTime   time.Time
	initialized    bool
	mu             sync.RWMutex
}

// UtilsManager handles utility functions and helper methods
type UtilsManager struct {
	logger     *logger.ModuleLogger
	strategy   *HTTPSyncStrategy // Reference to parent strategy for access to other modules
	cpuTracker *CPUTracker       // CPU usage tracking
}

// NewUtilsManager creates a new utilities manager
func NewUtilsManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *UtilsManager {
	return &UtilsManager{
		logger:     logger,
		strategy:   strategy,
		cpuTracker: &CPUTracker{},
	}
}

// Utility Functions for HTTP Strategy

// getTagDescription provides human-readable descriptions for common tags
func (u *UtilsManager) getTagDescription(tagKey string) string {
	// All descriptions removed for uniform tag display
	descriptions := map[string]string{
		// Tags display as "tag_name (X values)" without additional descriptions
	}
	
	if desc, exists := descriptions[tagKey]; exists {
		return desc
	}
	return "No description available"
}

// Version and Build Info

// VersionInfo holds parsed version and commit information
type VersionInfo struct {
	Version string
	Commit  string
}

// parseVersionInfo parses version and commit information from cliArgs
func (u *UtilsManager) parseVersionInfo() VersionInfo {
	version := cliArgs.Version
	commit := cliArgs.CommitHash
	
	// Linux/Makefile case: Version is properly set, use it directly
	if version != "" {
		return VersionInfo{
			Version: version,
			Commit:  formatCommitHash(commit),
		}
	}
	
	// Windows/no-Makefile case: Version is empty, try to extract from CommitHash
	var extractedCommitHash string
	if commit != "" {
		// Parse git describe format: "tag-commits-ghash-dirty"
		if strings.Contains(commit, "-g") {
			parts := strings.Split(commit, "-")
			for i, part := range parts {
				if strings.HasPrefix(part, "g") && i > 0 {
					// Version is everything before the commit count
					version = strings.Join(parts[:i-1], "-")
					// Extract commit hash (part starting with 'g')
					extractedCommitHash = part
					break
				}
			}
		} else {
			// If no -g, it might be just a tag or commit hash
			version = commit
			extractedCommitHash = commit
		}
	}
	
	// Final fallback
	if version == "" {
		version = "development"
	}
	
	// Format commit hash for display - use extracted hash, not full commit string
	var formattedCommit string
	if extractedCommitHash != "" {
		formattedCommit = formatCommitHash(extractedCommitHash)
	} else {
		formattedCommit = formatCommitHash(commit)
	}
	
	return VersionInfo{
		Version: version,
		Commit:  formattedCommit,
	}
}

// formatCommitHash formats a commit hash for human-readable display
func formatCommitHash(commit string) string {
	if commit == "" {
		return ""
	}
	
	// Handle single hash part with 'g' prefix (e.g., "g302b166")
	if strings.HasPrefix(commit, "g") && len(commit) > 1 {
		// Extract short hash (first 7 chars after 'g', or all if shorter)
		hashPart := commit[1:]
		if len(hashPart) >= 7 {
			return hashPart[:7]
		}
		return hashPart
	}
	
	// Handle git describe format: "tag-commits-ghash-dirty"
	if strings.Contains(commit, "-g") {
		parts := strings.Split(commit, "-")
		for i, part := range parts {
			if strings.HasPrefix(part, "g") && len(part) > 1 {
				// Extract short hash (first 7 chars after 'g')
				hashPart := part[1:]
				if len(hashPart) >= 7 {
					hashPart = hashPart[:7]
				}
				// Check if it's dirty
				isDirty := len(parts) > i+1 && parts[i+1] == "dirty"
				if isDirty {
					return fmt.Sprintf("%s (modified)", hashPart)
				}
				return hashPart
			}
		}
	}
	
	// Handle plain commit hash - take first 7 characters
	if len(commit) >= 7 {
		return commit[:7]
	}
	
	return commit
}

// formatDuration formats a duration in a human-readable format
func (u *UtilsManager) formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// CPU Measurement for system monitoring

// getCPUUsage calculates CPU usage percentage for the current process
func (u *UtilsManager) getCPUUsage() float64 {
	u.cpuTracker.mu.Lock()
	defer u.cpuTracker.mu.Unlock()
	
	// Get current CPU time for this process
	currentCPUTime, err := u.getCurrentProcessCPUTime()
	if err != nil {
		u.logger.Debug().Err(err).Msg("Failed to get CPU time")
		return 0.0
	}
	
	currentWallTime := time.Now()
	
	// If this is the first measurement, initialize and return 0
	if !u.cpuTracker.initialized {
		u.cpuTracker.lastCPUTime = currentCPUTime
		u.cpuTracker.lastWallTime = currentWallTime
		u.cpuTracker.initialized = true
		return 0.0
	}
	
	// Calculate deltas
	cpuDelta := currentCPUTime - u.cpuTracker.lastCPUTime
	wallDelta := currentWallTime.Sub(u.cpuTracker.lastWallTime)
	
	// Update for next calculation
	u.cpuTracker.lastCPUTime = currentCPUTime
	u.cpuTracker.lastWallTime = currentWallTime
	
	// Avoid division by zero
	if wallDelta == 0 {
		return 0.0
	}
	
	// Calculate CPU percentage
	cpuPercent := float64(cpuDelta) / float64(wallDelta) * 100.0
	
	// Ensure valid range: 0-100%
	if cpuPercent < 0.0 {
		u.logger.Debug().
			Float64("original_percent", cpuPercent).
			Int64("cpu_delta_ns", int64(cpuDelta)).
			Int64("wall_delta_ns", int64(wallDelta)).
			Msg("CPU percent was negative, clamping to 0%")
		cpuPercent = 0.0  // Handle negative values from system counter resets
	} else if cpuPercent > 100.0 {
		cpuPercent = 100.0  // Cap at 100% for single-core equivalent
	}
	
	return cpuPercent
}

// getCurrentProcessCPUTime gets CPU time for current process (cross-platform)
func (u *UtilsManager) getCurrentProcessCPUTime() (time.Duration, error) {
	switch runtime.GOOS {
	case "linux":
		return u.getCPUTimeLinux()
	case "darwin":
		return u.getCPUTimeDarwin()
	case "windows":
		return u.getCPUTimeWindows()
	default:
		// Fallback: use runtime stats for approximation
		return u.getCPUTimeRuntime()
	}
}

// getCPUTimeLinux reads CPU time from /proc/pid/stat
func (u *UtilsManager) getCPUTimeLinux() (time.Duration, error) {
	pid := os.Getpid()
	statFile := fmt.Sprintf("/proc/%d/stat", pid)
	
	data, err := os.ReadFile(statFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/%d/stat: %w", pid, err)
	}
	
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("insufficient fields in /proc/%d/stat", pid)
	}
	
	// Fields 13 and 14 are utime and stime (user and system CPU time in clock ticks)
	utime, err := strconv.ParseInt(fields[13], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse utime: %w", err)
	}
	
	stime, err := strconv.ParseInt(fields[14], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse stime: %w", err)
	}
	
	// Convert clock ticks to nanoseconds
	// Most Linux systems use 100 Hz (100 clock ticks per second)
	clockTicks := int64(100)
	totalTicks := utime + stime
	nanoseconds := (totalTicks * int64(time.Second)) / clockTicks
	
	return time.Duration(nanoseconds), nil
}

// getCPUTimeDarwin gets CPU time on macOS using runtime approximation
func (u *UtilsManager) getCPUTimeDarwin() (time.Duration, error) {
	// On macOS, without CGO, we use runtime stats as approximation
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// Estimate CPU time based on GC activity and runtime metrics
	// This is an approximation since exact CPU time requires platform-specific syscalls
	gcCPUTime := time.Duration(memStats.GCCPUFraction * float64(time.Since(u.cpuTracker.lastWallTime)))
	
	// Add base CPU time estimation
	estimatedCPUTime := gcCPUTime + time.Duration(memStats.NumGC)*time.Millisecond
	
	return estimatedCPUTime, nil
}

// getCPUTimeWindows gets CPU time on Windows using runtime approximation
func (u *UtilsManager) getCPUTimeWindows() (time.Duration, error) {
	// On Windows, without CGO, we use runtime stats as approximation
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// Estimate based on GC and goroutine activity
	gcTime := time.Duration(memStats.GCCPUFraction * float64(time.Since(u.cpuTracker.lastWallTime)))
	routineEstimate := time.Duration(runtime.NumGoroutine()) * time.Microsecond
	
	return gcTime + routineEstimate, nil
}

// getCPUTimeRuntime fallback using runtime statistics
func (u *UtilsManager) getCPUTimeRuntime() (time.Duration, error) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// Very rough approximation based on GC CPU fraction
	if u.cpuTracker.initialized {
		wallDelta := time.Since(u.cpuTracker.lastWallTime)
		estimatedCPUTime := time.Duration(memStats.GCCPUFraction * float64(wallDelta))
		return u.cpuTracker.lastCPUTime + estimatedCPUTime, nil
	}
	
	return time.Duration(memStats.GCCPUFraction * float64(time.Second)), nil
}

// Monitoring Format Handlers (Future Expansion)

// handleZabbixMetricsGET handles GET requests for Zabbix format metrics (placeholder)
func (u *UtilsManager) handleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := u.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	u.logger.Info().Msg("🔄 Zabbix endpoint - Request received")

	// TODO: Implement Zabbix format conversion
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error": "Zabbix format endpoint not yet implemented"}`))
}

// handlePrometheusMetricsGET handles GET requests for Prometheus format metrics (placeholder)
func (u *UtilsManager) handlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := u.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	u.logger.Info().Msg("🔄 Prometheus endpoint - Request received")

	// TODO: Implement Prometheus format conversion
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("# Prometheus format endpoint not yet implemented\n"))
}