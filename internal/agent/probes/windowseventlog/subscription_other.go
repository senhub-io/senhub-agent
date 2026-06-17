//go:build !windows

package windowseventlog

import (
	"context"
	"fmt"
	"runtime"

	"senhub-agent.go/internal/agent/services/logger"
)

// eventReader stub for non-Windows builds. The probe still compiles and
// registers everywhere — operators can keep one config that works across
// mixed-OS deployments — but instantiation fails loudly with a clear
// message on platforms without the Windows Event Log API (wevtapi).
// Same approach as the linux_logs probe's non-Linux stub.
type eventReader struct{}

func newEventReader(_ WindowsEventLogProbeConfig, _ *logger.ModuleLogger, _ string) (*eventReader, error) {
	return nil, fmt.Errorf("windows_eventlog probe is not supported on %s (requires the Windows Event Log API)", runtime.GOOS)
}

func (*eventReader) stop(context.Context) error { return nil }
