package status

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kardianos/service"
	"senhub-agent.go/internal/agent/services/logger"
)

// StatusHelper provides utilities for getting status information from CLI
type StatusHelper struct {
	logger *logger.ModuleLogger
}

// NewStatusHelper creates a new status helper
func NewStatusHelper(baseLogger *logger.Logger) *StatusHelper {
	moduleLogger := logger.NewModuleLogger(baseLogger, "status.helper")

	return &StatusHelper{
		logger: moduleLogger,
	}
}

// GetServiceStatus gets the basic service status using kardianos/service
func (h *StatusHelper) GetServiceStatus(svc service.Service) (string, error) {
	status, err := svc.Status()
	if err != nil {
		return "unknown", err
	}

	switch status {
	case service.StatusUnknown:
		return "unknown", nil
	case service.StatusRunning:
		return "running", nil
	case service.StatusStopped:
		return "stopped", nil
	default:
		return fmt.Sprintf("status_%d", int(status)), nil
	}
}

// GetDetailedStatusFromHTTP attempts to get detailed status from running HTTP strategy
func (h *StatusHelper) GetDetailedStatusFromHTTP(agentKey string, port int) (*SystemStatus, error) {
	if agentKey == "" {
		return nil, fmt.Errorf("agent key required for detailed status")
	}

	// Try to connect to local HTTP endpoint
	url := fmt.Sprintf("http://localhost:%d/api/%s/info/system", port, agentKey)

	h.logger.Debug().
		Str("url", url).
		Msg("Attempting to get detailed status from HTTP endpoint")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse the HTTP strategy's system info response and convert to our format
	var httpResponse HTTPSystemInfoResponse
	if err := json.Unmarshal(body, &httpResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to our SystemStatus format
	systemStatus := h.convertHTTPResponseToSystemStatus(httpResponse)

	// Enrich with individual probe status
	if probes, err := h.GetDetailedProbeStatusFromHTTP(agentKey, port); err == nil {
		systemStatus.Probes = probes
	} else {
		h.logger.Debug().Err(err).Msg("Could not get detailed probe status, using summary")
	}

	h.logger.Debug().
		Int("probe_count", len(systemStatus.Probes)).
		Str("health", systemStatus.Health.Status).
		Msg("Successfully retrieved detailed status from HTTP endpoint")

	return &systemStatus, nil
}

// HTTPSystemInfoResponse matches the HTTP strategy's system info response
// This is a simplified version - in practice, we'd import the actual types
type HTTPSystemInfoResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Port      int    `json:"port"`
	Uptime    string `json:"uptime"`
	Health    struct {
		Status    string            `json:"status"`
		Timestamp int64             `json:"timestamp"`
		Version   string            `json:"version"`
		Services  map[string]string `json:"services"`
	} `json:"health"`
	Cache struct {
		TotalMetrics int    `json:"total_metrics"`
		ProbeCount   int    `json:"probe_count"`
		TTL          string `json:"ttl"`
	} `json:"cache"`
	Resources struct {
		MemoryUsageMB float64 `json:"memory_usage_mb"`
		CPUPercent    float64 `json:"cpu_percent"`
		Goroutines    int     `json:"goroutines"`
	} `json:"resources"`
}

// convertHTTPResponseToSystemStatus converts HTTP strategy response to our format
func (h *StatusHelper) convertHTTPResponseToSystemStatus(httpResp HTTPSystemInfoResponse) SystemStatus {
	// Determine connection mode from health services (which includes mode)
	var mode string
	if modeStr, ok := httpResp.Health.Services["mode"]; ok {
		mode = modeStr
	} else {
		// Fallback: try to detect from version string
		if strings.Contains(httpResp.Health.Version, "offline") {
			mode = "offline"
		} else {
			mode = "online"
		}
	}

	return SystemStatus{
		Health: HealthInfo{
			Status:    httpResp.Health.Status,
			Timestamp: time.Unix(httpResp.Health.Timestamp, 0),
		},
		Connection: ConnectionInfo{
			Mode:   mode,
			Source: h.determineSource(mode),
			Status: h.determineConnectionStatus(mode),
		},
		Probes: nil, // Will be populated by caller via GetDetailedProbeStatusFromHTTP
		Performance: PerformanceInfo{
			Uptime:        httpResp.Uptime,
			MemoryUsageMB: httpResp.Resources.MemoryUsageMB,
			CPUPercent:    httpResp.Resources.CPUPercent,
			Goroutines:    httpResp.Resources.Goroutines,
			CacheEntries:  httpResp.Cache.TotalMetrics,
		},
		Agent: AgentInfo{
			Version:   httpResp.Version,
			Commit:    httpResp.Commit,
			GoVersion: httpResp.GoVersion,
			OS:        httpResp.OS,
			Arch:      httpResp.Arch,
		},
	}
}

// determineSource determines configuration source based on mode
func (h *StatusHelper) determineSource(mode string) string {
	switch mode {
	case "online":
		return "remote_server"
	case "offline":
		return "local_config"
	default:
		return "unknown"
	}
}

// determineConnectionStatus determines connection status based on mode
func (h *StatusHelper) determineConnectionStatus(mode string) string {
	switch mode {
	case "online":
		return "connected"
	case "offline":
		return "local"
	default:
		return "unknown"
	}
}

// OTLPInfo mirrors the JSON shape returned by /api/{agentkey}/info/otlp.
// Defined here (rather than importing from the http strategy) so the
// CLI doesn't pull in the entire HTTP-server package graph.
type OTLPInfo struct {
	Pipeline struct {
		MetricsPushedTotal uint64            `json:"metrics_pushed_total"`
		LogsPushedTotal    uint64            `json:"logs_pushed_total"`
		ExportErrorsTotal  uint64            `json:"export_errors_total"`
		DroppedTotal       uint64            `json:"dropped_total"`
		DroppedByReason    map[string]uint64 `json:"dropped_by_reason"`
	} `json:"pipeline"`
	Store struct {
		Size               int64   `json:"size"`
		LogBufferFillRatio float64 `json:"log_buffer_fill_ratio"`
	} `json:"store"`
	ExportDuration struct {
		LastMs float64 `json:"last_ms"`
		MeanMs float64 `json:"mean_ms"`
	} `json:"export_duration"`
	Checkpoint struct {
		SizeBytes          int64             `json:"size_bytes"`
		LastSaveAgeSeconds float64           `json:"last_save_age_seconds"`
		RestoredEntries    int64             `json:"restored_entries"`
		ErrorsTotal        uint64            `json:"errors_total"`
		ErrorsByStage      map[string]uint64 `json:"errors_by_stage"`
	} `json:"checkpoint"`
	Parallel struct {
		SubBatches int32 `json:"sub_batches"`
	} `json:"parallel"`
}

// GetOTLPInfoFromHTTP fetches the OTLP self-metric snapshot from the
// running agent's HTTP strategy. Returns an error if the agent is not
// reachable or the HTTP strategy is disabled.
func (h *StatusHelper) GetOTLPInfoFromHTTP(agentKey string, port int) (*OTLPInfo, error) {
	if agentKey == "" {
		return nil, fmt.Errorf("agent key required for OTLP info")
	}

	url := fmt.Sprintf("http://localhost:%d/api/%s/info/otlp", port, agentKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var info OTLPInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse OTLP info response: %w", err)
	}
	return &info, nil
}

// GetDetailedProbeStatusFromHTTP gets detailed probe information
func (h *StatusHelper) GetDetailedProbeStatusFromHTTP(agentKey string, port int) ([]ProbeStatus, error) {
	if agentKey == "" {
		return nil, fmt.Errorf("agent key required for probe status")
	}

	// Try to get probe information from HTTP endpoint
	url := fmt.Sprintf("http://localhost:%d/api/%s/info/probes", port, agentKey)

	h.logger.Debug().
		Str("url", url).
		Msg("Attempting to get probe status from HTTP endpoint")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse probe response
	var probeResponse struct {
		Probes []struct {
			Name         string `json:"name"`
			MetricsCount int    `json:"metrics_count"`
			LastUpdate   string `json:"last_update,omitempty"`
		} `json:"probes"`
	}

	if err := json.Unmarshal(body, &probeResponse); err != nil {
		return nil, fmt.Errorf("failed to parse probe response: %w", err)
	}

	// Convert to our format
	var probeStatuses []ProbeStatus
	for _, probe := range probeResponse.Probes {
		status := "active"
		if probe.MetricsCount == 0 {
			status = "inactive"
		}

		var lastUpdate time.Time
		if probe.LastUpdate != "" {
			if parsed, err := time.Parse(time.RFC3339, probe.LastUpdate); err == nil {
				lastUpdate = parsed
			}
		}

		probeStatuses = append(probeStatuses, ProbeStatus{
			Name:         probe.Name,
			Status:       status,
			MetricsCount: probe.MetricsCount,
			LastUpdate:   lastUpdate,
		})
	}

	h.logger.Debug().
		Int("probe_count", len(probeStatuses)).
		Msg("Successfully retrieved probe status from HTTP endpoint")

	return probeStatuses, nil
}
