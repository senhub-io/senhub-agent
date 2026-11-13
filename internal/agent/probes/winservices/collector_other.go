//go:build !windows

package winservices

import (
	"fmt"

	"senhub-agent.go/internal/agent/probes/subcollect"
	"senhub-agent.go/internal/agent/services/logger"
)

// collectServices is the non-Windows stub. The Service Control Manager only
// exists on Windows, so on any other OS the probe reports its outage via
// senhub.winservices.up=0 (the caller turns this error into up=0) rather
// than silently disabling itself.
func collectServices(_ []string, _ *logger.ModuleLogger, _ *subcollect.Tracker) ([]serviceState, error) {
	return nil, fmt.Errorf("winservices: the Service Control Manager is only available on Windows")
}
