//go:build !linux

package linuxlogs

import (
	"context"
	"fmt"
	"runtime"

	"senhub-agent.go/internal/agent/services/logger"
)

// journalReader stub for non-Linux builds. The probe still compiles
// and registers everywhere — operators can keep one config that works
// across mixed-OS deployments — but instantiation fails loudly with
// a clear message on platforms without systemd-journald.
type journalReader struct{}

func newJournalReader(_ LinuxLogsProbeConfig, _ *logger.ModuleLogger, _ string) (*journalReader, error) {
	return nil, fmt.Errorf("linux_logs probe is not supported on %s (requires systemd-journald)", runtime.GOOS)
}

func (*journalReader) stop(context.Context) error { return nil }
