//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/auto_update"
	"senhub-agent.go/internal/agent/services/logger"
)

// runUpdateWatch is the detached watcher spawned before the service exits
// into a new binary: it rolls back to the .previous binary if the service
// never comes up.
func runUpdateWatch(args []string) int {
	a, perr := parseUpdateWatchArgs(args)
	if a.stateDir == "" {
		return 2
	}
	logFile, err := os.OpenFile(filepath.Join(a.stateDir, "update-watch.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 1
	}
	defer logFile.Close() //nolint:errcheck // best-effort close
	logf := func(format string, v ...any) {
		fmt.Fprintf(logFile, time.Now().Format(time.RFC3339)+" "+format+"\n", v...)
	}
	logf("update-watch started binary=%q version=%q from=%q", a.binary, a.version, a.from)
	if perr != nil {
		logf("refused: %v", perr)
		return 2
	}

	w := updateWatch{
		running: func() (string, bool) {
			m, err := mgr.Connect()
			if err != nil {
				return "", false
			}
			defer m.Disconnect() //nolint:errcheck // best-effort release
			s, err := m.OpenService(windowsServiceName)
			if err != nil {
				return "", false
			}
			defer s.Close() //nolint:errcheck // best-effort release
			st, err := s.Query()
			if err != nil || st.State != svc.Running {
				return "", false
			}
			return strconv.Itoa(int(st.ProcessId)), true
		},
		exePath: func(pid string) (string, error) {
			p, err := strconv.Atoi(pid)
			if err != nil {
				return "", err
			}
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p))
			if err != nil {
				return "", err
			}
			defer windows.CloseHandle(h) //nolint:errcheck // best-effort release
			buf := make([]uint16, windows.MAX_PATH)
			size := uint32(len(buf))
			if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
				return "", err
			}
			return windows.UTF16ToString(buf[:size]), nil
		},
		restore: func() error {
			log := logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "update.watch")
			return auto_update.RestoreAfterFailedStart(a.stateDir, a.binary, a.version, a.from, log)
		},
		start: func() (string, error) {
			return restartServiceAfterUpdate(true)
		},
		binary: a.binary,
		wait:   90 * time.Second,
		settle: 15 * time.Second,
		poll:   time.Second,
		now:    time.Now,
		sleep:  time.Sleep,
	}
	outcome, err := w.run()
	if err != nil {
		logf("outcome=%q error=%v", outcome, err)
		return 1
	}
	logf("outcome=%s", outcome)
	return 0
}
