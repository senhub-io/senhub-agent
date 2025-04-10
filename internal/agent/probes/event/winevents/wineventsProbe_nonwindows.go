//go:build !windows
// +build !windows

package winevents

import (
	"context"

	"senhub-agent.go/internal/agent/services/data_store"
)

// isWindows returns false for non-Windows platforms
func isWindows() bool {
	return false
}

// collectNonWindowsEvents is a no-op implementation for non-Windows platforms
func collectNonWindowsEvents(p *WinEventProbe) ([]data_store.DataPoint, error) {
	// This should never be called since ShouldStart() returns false on non-Windows,
	// but is provided as a safety measure
	return nil, nil
}

// startNonWindowsEventSubscriptions is a no-op implementation for non-Windows platforms
func startNonWindowsEventSubscriptions(p *WinEventProbe, quitChannel chan struct{}) error {
	// This should never be called since ShouldStart() returns false on non-Windows,
	// but is provided as a safety measure
	return nil
}

// shutdownNonWindowsEventSubscriptions is a no-op implementation for non-Windows platforms
func shutdownNonWindowsEventSubscriptions(p *WinEventProbe, ctx context.Context) error {
	// This should never be called since ShouldStart() returns false on non-Windows,
	// but is provided as a safety measure
	return nil
}

// Initialize platform-specific implementations for non-Windows
func init() {
	collectImpl = collectNonWindowsEvents
	startImpl = startNonWindowsEventSubscriptions
	shutdownImpl = shutdownNonWindowsEventSubscriptions
}