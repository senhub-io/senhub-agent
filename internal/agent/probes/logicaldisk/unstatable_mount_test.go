//go:build !windows

package logicaldisk

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func collectorWithLog(buf *bytes.Buffer) *unixLogicalDiskCollector {
	zl := zerolog.New(buf)
	return &unixLogicalDiskCollector{logger: logger.NewModuleLogger((*logger.Logger)(&zl), "probe.logicaldisk.test")}
}

func warnCount(out string) int { return strings.Count(out, `"level":"warn"`) }

// A per-user tmpfs the service account cannot read is routine: no warning,
// on any cycle (#591).
func TestReportUnstatable_RefusedUserRuntimeDirIsNotAWarning(t *testing.T) {
	var buf bytes.Buffer
	c := collectorWithLog(&buf)
	m := mountInfo{mountpoint: "/run/user/1000", fstype: "tmpfs"}
	for i := 0; i < 3; i++ {
		c.reportUnstatable(m, fmt.Errorf("statfs: %w", syscall.EACCES))
	}
	if n := warnCount(buf.String()); n != 0 {
		t.Errorf("%d warnings for a refused /run/user mount, want none:\n%s", n, buf.String())
	}
}

// Any other mount that cannot be read is named once, not every cycle.
func TestReportUnstatable_OtherMountWarnsOncePerMount(t *testing.T) {
	var buf bytes.Buffer
	c := collectorWithLog(&buf)
	data := mountInfo{mountpoint: "/data", fstype: "xfs"}
	other := mountInfo{mountpoint: "/srv", fstype: "ext4"}
	for i := 0; i < 4; i++ {
		c.reportUnstatable(data, errors.New("input/output error"))
	}
	c.reportUnstatable(other, errors.New("input/output error"))
	if n := warnCount(buf.String()); n != 2 {
		t.Errorf("%d warnings, want one per distinct mount (2):\n%s", n, buf.String())
	}
}

// A mount that answers again and then fails anew is worth another warning.
func TestReportUnstatable_RecoveredMountWarnsAgain(t *testing.T) {
	var buf bytes.Buffer
	c := collectorWithLog(&buf)
	m := mountInfo{mountpoint: "/data", fstype: "xfs"}
	c.reportUnstatable(m, errors.New("boom"))
	c.unstatable.Delete(m.mountpoint)
	c.reportUnstatable(m, errors.New("boom"))
	if n := warnCount(buf.String()); n != 2 {
		t.Errorf("%d warnings, want 2", n)
	}
}

// EACCES outside /run/user is not routine and still warns.
func TestReportUnstatable_EACCESElsewhereStillWarns(t *testing.T) {
	var buf bytes.Buffer
	c := collectorWithLog(&buf)
	c.reportUnstatable(mountInfo{mountpoint: "/mnt/secret", fstype: "ext4"}, fmt.Errorf("statfs: %w", syscall.EACCES))
	if n := warnCount(buf.String()); n != 1 {
		t.Errorf("%d warnings, want 1", n)
	}
}
