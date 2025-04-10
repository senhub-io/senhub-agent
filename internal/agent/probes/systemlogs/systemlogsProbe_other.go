//go:build !windows && !linux && !darwin
// +build !windows,!linux,!darwin

package systemlogs

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

// isSourceSupported checks if a log source is supported on this platform
func isSourceSupported(source LogSource) bool {
	// On other platforms, only traditional syslog is supported
	return source == LogSourceSyslog
}

// collectSystemLogs is the fallback implementation that uses only basic syslog
func collectSystemLogs(p *SystemLogsProbe) ([]data_store.DataPoint, error) {
	p.logger.Debug().Msg("Collecting system logs from generic platform")
	
	// For generic platforms, we only implement very basic syslog reading
	// This would typically be reading from /var/log/syslog or similar
	
	// This implementation is minimal and would need to be expanded
	
	// No entries collected in this placeholder implementation
	return []data_store.DataPoint{}, nil
}

// startSystemLogSubscriptions is the generic implementation of OnStart
func startSystemLogSubscriptions(p *SystemLogsProbe, quitChannel chan struct{}) error {
	p.logger.Debug().Msg("Starting System Logs probe on generic platform")
	
	// Basic initialization for other platforms
	
	return nil
}

// shutdownSystemLogSubscriptions is the generic implementation of OnShutdown
func shutdownSystemLogSubscriptions(p *SystemLogsProbe, ctx context.Context) error {
	p.logger.Info().Msg("Stopping System Logs probe on generic platform")
	
	// Clean up any resources
	
	return nil
}

// Initialize platform-specific implementations
func init() {
	collectImpl = collectSystemLogs
	startImpl = startSystemLogSubscriptions
	shutdownImpl = shutdownSystemLogSubscriptions
}