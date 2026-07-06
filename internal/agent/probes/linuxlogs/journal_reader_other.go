//go:build !linux

package linuxlogs

import (
	"context"
	"fmt"
	"runtime"
	"time"

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

func (*journalReader) healthErr() error { return nil }

// The following exist only so the cross-platform supervisor compiles on
// non-Linux. newJournalReader always errors here, so a stub reader is never
// actually supervised.
func (*journalReader) waitCh() <-chan struct{}  { return nil }
func (*journalReader) exitedUnexpectedly() bool { return false }
func (*journalReader) uptime() time.Duration    { return 0 }
