package status

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CLIFormatter formats status information for command line display
type CLIFormatter struct {
	titleCaser cases.Caser
}

// NewCLIFormatter creates a new CLI formatter
func NewCLIFormatter() *CLIFormatter {
	return &CLIFormatter{
		titleCaser: cases.Title(language.English),
	}
}

// FormatSystemStatus formats complete system status for CLI display
func (f *CLIFormatter) FormatSystemStatus(status SystemStatus) string {
	var output strings.Builder

	// Header
	if runtime.GOOS == "windows" {
		output.WriteString("SenHub Agent Status\n")
	} else {
		output.WriteString("📊 SenHub Agent Status\n")
	}
	output.WriteString(strings.Repeat("=", 50) + "\n\n")

	// Agent Information
	output.WriteString(f.formatAgentInfo(status.Agent))
	output.WriteString("\n")

	// Health Status
	output.WriteString(f.formatHealthInfo(status.Health))
	output.WriteString("\n")

	// Connection Information
	output.WriteString(f.formatConnectionInfo(status.Connection))
	output.WriteString("\n")

	// Performance Metrics
	output.WriteString(f.formatPerformanceInfo(status.Performance))

	// Probe Status (only if we have probes)
	if len(status.Probes) > 0 {
		output.WriteString("\n")
		output.WriteString(f.FormatProbeStatuses(status.Probes))
	}

	return output.String()
}

// FormatBasicStatus formats basic status information for CLI display
func (f *CLIFormatter) FormatBasicStatus(health HealthInfo, agent AgentInfo) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("SenHub Agent Status\n")
	} else {
		output.WriteString("📊 SenHub Agent Status\n")
	}
	output.WriteString(strings.Repeat("=", 30) + "\n\n")

	// Basic health
	statusIcon := f.getHealthIcon(health.Status)
	output.WriteString(fmt.Sprintf("Status: %s %s\n", statusIcon, f.titleCaser.String(health.Status)))

	if health.Message != "" {
		output.WriteString(fmt.Sprintf("       %s\n", health.Message))
	}

	// Basic agent info
	output.WriteString(fmt.Sprintf("Version: %s (%s)\n", agent.Version, agent.Commit))
	output.WriteString(fmt.Sprintf("Platform: %s/%s\n", agent.OS, agent.Arch))

	return output.String()
}

// formatAgentInfo formats agent information section
func (f *CLIFormatter) formatAgentInfo(agent AgentInfo) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("Agent Information\n")
	} else {
		output.WriteString("🔧 Agent Information\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")
	output.WriteString(fmt.Sprintf("Version:    %s\n", agent.Version))
	output.WriteString(fmt.Sprintf("Commit:     %s\n", agent.Commit))
	// Go version hidden for security reasons
	output.WriteString(fmt.Sprintf("Platform:   %s/%s\n", agent.OS, agent.Arch))

	if agent.BuildTime != "" {
		output.WriteString(fmt.Sprintf("Built:      %s\n", agent.BuildTime))
	}

	return output.String()
}

// formatHealthInfo formats health information section
func (f *CLIFormatter) formatHealthInfo(health HealthInfo) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("System Health\n")
	} else {
		output.WriteString("💚 System Health\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")

	statusIcon := f.getHealthIcon(health.Status)
	output.WriteString(fmt.Sprintf("Status:     %s %s\n", statusIcon, f.titleCaser.String(health.Status)))
	output.WriteString(fmt.Sprintf("Checked:    %s\n", health.Timestamp.Format("2006-01-02 15:04:05")))

	if health.Message != "" {
		output.WriteString(fmt.Sprintf("Message:    %s\n", health.Message))
	}

	return output.String()
}

// formatConnectionInfo formats connection information section
func (f *CLIFormatter) formatConnectionInfo(conn ConnectionInfo) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("Connection\n")
	} else {
		output.WriteString("🌐 Connection\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")

	modeIcon := f.getConnectionIcon(conn.Mode)
	if modeIcon != "" {
		output.WriteString(fmt.Sprintf("Mode:       %s %s\n", modeIcon, f.titleCaser.String(conn.Mode)))
	} else {
		output.WriteString(fmt.Sprintf("Mode:       %s\n", f.titleCaser.String(conn.Mode)))
	}
	output.WriteString(fmt.Sprintf("Source:     %s\n", f.formatSource(conn.Source)))
	output.WriteString(fmt.Sprintf("Status:     %s\n", f.formatConnectionStatus(conn.Status)))

	if conn.DashboardURL != "" {
		output.WriteString(fmt.Sprintf("Dashboard:  %s\n", conn.DashboardURL))
	}

	return output.String()
}

// formatPerformanceInfo formats performance metrics section
func (f *CLIFormatter) formatPerformanceInfo(perf PerformanceInfo) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("Performance\n")
	} else {
		output.WriteString("⚡ Performance\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")
	output.WriteString(fmt.Sprintf("Uptime:     %s\n", perf.Uptime))
	output.WriteString(fmt.Sprintf("Memory:     %.1f MB\n", perf.MemoryUsageMB))
	output.WriteString(fmt.Sprintf("Goroutines: %d\n", perf.Goroutines))

	if perf.CPUPercent > 0 {
		output.WriteString(fmt.Sprintf("CPU:        %.1f%%\n", perf.CPUPercent))
	}

	if perf.CacheEntries > 0 {
		output.WriteString(fmt.Sprintf("Cache:      %d entries\n", perf.CacheEntries))
	}

	return output.String()
}

// FormatProbeStatuses formats probe status section
func (f *CLIFormatter) FormatProbeStatuses(probes []ProbeStatus) string {
	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("Probes\n")
	} else {
		output.WriteString("🔍 Probes\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")

	if len(probes) == 0 {
		output.WriteString("No probes configured\n")
		return output.String()
	}

	// Summary line
	activeCount := 0
	errorCount := 0
	totalMetrics := 0

	for _, probe := range probes {
		if probe.Status == "active" {
			activeCount++
		} else if probe.Status == "error" {
			errorCount++
		}
		totalMetrics += probe.MetricsCount
	}

	output.WriteString(fmt.Sprintf("Total: %d probes (%d active, %d errors)\n",
		len(probes), activeCount, errorCount))
	output.WriteString(fmt.Sprintf("Metrics: %d total\n\n", totalMetrics))

	// Individual probe details
	for _, probe := range probes {
		statusIcon := f.getProbeIcon(probe.Status)
		if statusIcon != "" {
			output.WriteString(fmt.Sprintf("  %s %s\n", statusIcon, probe.Name))
		} else {
			output.WriteString(fmt.Sprintf("  %s\n", probe.Name))
		}
		output.WriteString(fmt.Sprintf("     Status: %s\n", f.titleCaser.String(probe.Status)))
		output.WriteString(fmt.Sprintf("     Metrics: %d\n", probe.MetricsCount))

		if !probe.LastUpdate.IsZero() {
			timeSince := time.Since(probe.LastUpdate)
			output.WriteString(fmt.Sprintf("     Updated: %s ago\n", f.formatDuration(timeSince)))
		}

		if probe.LastError != "" {
			output.WriteString(fmt.Sprintf("     Error: %s\n", probe.LastError))
		}

		output.WriteString("\n")
	}

	return output.String()
}

// FormatOTLPInfo renders the OTLP self-metric snapshot for the CLI
// `agent status --otlp` view. Layout mirrors the dashboard card:
// Pipeline (counters), Store (size + log fill), Export (last/mean),
// Checkpoint (file + restore), Parallel (sub-batches).
//
// Returns an empty string when info is nil so callers can chain it
// unconditionally after FormatSystemStatus.
func (f *CLIFormatter) FormatOTLPInfo(info *OTLPInfo) string {
	if info == nil {
		return ""
	}

	var output strings.Builder

	if runtime.GOOS == "windows" {
		output.WriteString("OTLP Pipeline\n")
	} else {
		output.WriteString("📡 OTLP Pipeline\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")

	output.WriteString(fmt.Sprintf("Metrics pushed:    %d\n", info.Pipeline.MetricsPushedTotal))
	output.WriteString(fmt.Sprintf("Logs pushed:       %d\n", info.Pipeline.LogsPushedTotal))
	output.WriteString(fmt.Sprintf("Export errors:     %d\n", info.Pipeline.ExportErrorsTotal))
	output.WriteString(fmt.Sprintf("Dropped:           %d\n", info.Pipeline.DroppedTotal))
	if len(info.Pipeline.DroppedByReason) > 0 {
		for _, reason := range sortedKeys(info.Pipeline.DroppedByReason) {
			output.WriteString(fmt.Sprintf("  by %-14s %d\n", reason+":", info.Pipeline.DroppedByReason[reason]))
		}
	}

	output.WriteString("\n")
	output.WriteString("Store size:        " + fmt.Sprintf("%d series", info.Store.Size) + "\n")
	output.WriteString(fmt.Sprintf("Log buffer fill:   %.1f%%\n", info.Store.LogBufferFillRatio*100))
	output.WriteString(fmt.Sprintf("Export last:       %s\n", formatMsDuration(info.ExportDuration.LastMs)))
	output.WriteString(fmt.Sprintf("Export mean:       %s\n", formatMsDuration(info.ExportDuration.MeanMs)))

	output.WriteString("\n")
	if runtime.GOOS == "windows" {
		output.WriteString("Checkpoint\n")
	} else {
		output.WriteString("💾 Checkpoint\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")
	if info.Checkpoint.SizeBytes == 0 && info.Checkpoint.LastSaveAgeSeconds == 0 && info.Checkpoint.RestoredEntries == 0 {
		output.WriteString("Disabled (no save observed)\n")
	} else {
		output.WriteString(fmt.Sprintf("Size:              %s\n", formatBytes(info.Checkpoint.SizeBytes)))
		if info.Checkpoint.LastSaveAgeSeconds > 0 {
			output.WriteString(fmt.Sprintf("Last save:         %s ago\n", formatSecDuration(info.Checkpoint.LastSaveAgeSeconds)))
		} else {
			output.WriteString("Last save:         never\n")
		}
		output.WriteString(fmt.Sprintf("Restored at boot:  %d entries\n", info.Checkpoint.RestoredEntries))
	}
	if info.Checkpoint.ErrorsTotal > 0 {
		output.WriteString(fmt.Sprintf("Errors:            %d\n", info.Checkpoint.ErrorsTotal))
		for _, stage := range sortedKeys(info.Checkpoint.ErrorsByStage) {
			output.WriteString(fmt.Sprintf("  at %-14s %d\n", stage+":", info.Checkpoint.ErrorsByStage[stage]))
		}
	}

	output.WriteString("\n")
	if runtime.GOOS == "windows" {
		output.WriteString("Parallel export\n")
	} else {
		output.WriteString("🔀 Parallel export\n")
	}
	output.WriteString(strings.Repeat("-", 30) + "\n")
	if info.Parallel.SubBatches <= 1 {
		output.WriteString(fmt.Sprintf("Sub-batches:       %d (single-batch path)\n", info.Parallel.SubBatches))
	} else {
		output.WriteString(fmt.Sprintf("Sub-batches:       %d (fan-out by probe)\n", info.Parallel.SubBatches))
	}

	return output.String()
}

func sortedKeys(m map[string]uint64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// simple insertion sort — maps here have ≤10 entries in practice.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatMsDuration(ms float64) string {
	if ms <= 0 {
		return "—"
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0f ms", ms)
	}
	return fmt.Sprintf("%.2f s", ms/1000)
}

func formatSecDuration(secs float64) string {
	d := time.Duration(secs * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// Helper functions for icons and formatting

func (f *CLIFormatter) getHealthIcon(status string) string {
	// No icons on Windows due to compatibility issues
	if runtime.GOOS == "windows" {
		return ""
	}

	switch status {
	case "healthy":
		return "✅"
	case "degraded":
		return "⚠️"
	case "unhealthy":
		return "❌"
	default:
		return "❓"
	}
}

func (f *CLIFormatter) getConnectionIcon(mode string) string {
	// No icons on Windows due to compatibility issues
	if runtime.GOOS == "windows" {
		return ""
	}

	switch mode {
	case "online":
		return "🌐"
	case "offline":
		return "📱"
	default:
		return "❓"
	}
}

func (f *CLIFormatter) getProbeIcon(status string) string {
	// No icons on Windows due to compatibility issues
	if runtime.GOOS == "windows" {
		return ""
	}

	switch status {
	case "active":
		return "✅"
	case "inactive":
		return "⏸️"
	case "error":
		return "❌"
	default:
		return "❓"
	}
}

func (f *CLIFormatter) formatSource(source string) string {
	switch source {
	case "remote_server":
		return "Remote server"
	case "local_config":
		return "Local configuration"
	case "unknown":
		return "Unknown"
	default:
		return source
	}
}

func (f *CLIFormatter) formatConnectionStatus(status string) string {
	switch status {
	case "connected":
		if runtime.GOOS == "windows" {
			return "Connected"
		}
		return "✅ Connected"
	case "disconnected":
		if runtime.GOOS == "windows" {
			return "Disconnected"
		}
		return "❌ Disconnected"
	case "local":
		if runtime.GOOS == "windows" {
			return "Local mode"
		}
		return "📁 Local mode"
	default:
		return status
	}
}

func (f *CLIFormatter) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd", days)
	}
}
