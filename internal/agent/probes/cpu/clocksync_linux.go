//go:build linux

package cpu

import (
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const clockSourceCacheTTL = 10 * time.Minute

type linuxClockSync struct {
	root           string
	source         string
	sourceCached   bool
	sourceCachedAt time.Time
}

func newClockSync() func() (clockSync, error) {
	s := &linuxClockSync{root: "/"}
	return s.read
}

func (s *linuxClockSync) read() (clockSync, error) {
	if !s.sourceCached || time.Since(s.sourceCachedAt) > clockSourceCacheTTL {
		s.source = detectClockSource(s.root)
		s.sourceCachedAt = time.Now()
		s.sourceCached = true
	}

	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return clockSync{}, err
	}

	synced, estErr, maxErr := clockSyncFromTimex(state, tx)
	return clockSync{
		source:   s.source,
		synced:   synced,
		offset:   estErr,
		maxError: maxErr,
	}, nil
}

func clockSyncFromTimex(state int, tx unix.Timex) (synced bool, estErr, maxErr time.Duration) {
	synced = state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0
	estErr = time.Duration(tx.Esterror) * time.Microsecond
	maxErr = time.Duration(tx.Maxerror) * time.Microsecond
	return
}

func detectClockSource(root string) string {
	if fileExists(filepath.Join(root, "run/chrony")) ||
		fileExists(filepath.Join(root, "var/run/chrony/chronyd.pid")) {
		return "chrony"
	}
	if fileExists(filepath.Join(root, "var/run/ntpd.pid")) {
		return "ntpd"
	}
	if fileExists(filepath.Join(root, "run/systemd/timesync/synchronized")) ||
		fileExists(filepath.Join(root, "run/systemd/timesync")) {
		return "timesyncd"
	}
	return "unknown"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
