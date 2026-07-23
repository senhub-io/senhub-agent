//go:build !linux && !windows

package osupdates

import (
	"context"
	"fmt"
	"runtime"

	"senhub-agent.go/internal/agent/services/logger"
)

// Non-Linux, non-Windows builds (darwin is a local-only dev target):
// the collector always fails, so the probe emits senhub.os.updates.up=0
// every cycle — visible but explicitly unsupported, per the graceful
// degradation contract.
type unsupportedCollector struct{}

func (unsupportedCollector) collect(context.Context) (updatesStatus, error) {
	return updatesStatus{}, fmt.Errorf("os_updates probe is not supported on %s", runtime.GOOS)
}

func newOSUpdatesCollector(_ *logger.ModuleLogger) updatesCollector {
	return unsupportedCollector{}
}
