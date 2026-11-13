//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const windowsServiceName = "senhub-agent"

// restartServiceAfterUpdate restarts the SCM service after update has
// installed the new binary. An MSI-managed host is left alone because
// msiexec restarts the service itself.
func restartServiceAfterUpdate() (string, error) {
	return serviceRestarter{
		installed: func() bool {
			if hostMsiManaged() {
				return false
			}
			m, err := mgr.Connect()
			if err != nil {
				return false
			}
			defer m.Disconnect() //nolint:errcheck // best-effort release
			s, err := m.OpenService(windowsServiceName)
			if err != nil {
				return false
			}
			defer s.Close() //nolint:errcheck // best-effort release
			return true
		},
		isActive: func() bool {
			m, err := mgr.Connect()
			if err != nil {
				return false
			}
			defer m.Disconnect() //nolint:errcheck // best-effort release
			s, err := m.OpenService(windowsServiceName)
			if err != nil {
				return false
			}
			defer s.Close() //nolint:errcheck // best-effort release
			st, err := s.Query()
			if err != nil {
				return false
			}
			return st.State == svc.Running
		},
		mainPID: func() string {
			m, err := mgr.Connect()
			if err != nil {
				return ""
			}
			defer m.Disconnect() //nolint:errcheck // best-effort release
			s, err := m.OpenService(windowsServiceName)
			if err != nil {
				return ""
			}
			defer s.Close() //nolint:errcheck // best-effort release
			st, err := s.Query()
			if err != nil {
				return ""
			}
			return strconv.FormatUint(uint64(st.ProcessId), 10)
		},
		restart: func() error {
			m, err := mgr.Connect()
			if err != nil {
				return fmt.Errorf("connecting to the service manager: %w", err)
			}
			defer m.Disconnect() //nolint:errcheck // best-effort release
			s, err := m.OpenService(windowsServiceName)
			if err != nil {
				return fmt.Errorf("opening service %s: %w", windowsServiceName, err)
			}
			defer s.Close() //nolint:errcheck // best-effort release
			if _, err := s.Control(svc.Stop); err != nil {
				return fmt.Errorf("sending stop to %s: %w", windowsServiceName, err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				st, err := s.Query()
				if err == nil && st.State == svc.Stopped {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("service %s did not stop within 30s", windowsServiceName)
				}
				time.Sleep(500 * time.Millisecond)
			}
			if err := s.Start(); err != nil {
				return fmt.Errorf("starting service %s: %w", windowsServiceName, err)
			}
			return nil
		},
		exePath: func(pid string) (string, error) {
			p, err := strconv.Atoi(pid)
			if err != nil {
				return "", fmt.Errorf("parsing pid %q: %w", pid, err)
			}
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p))
			if err != nil {
				return "", err
			}
			defer windows.CloseHandle(h) //nolint:errcheck // best-effort release
			buf := make([]uint16, 1024)
			size := uint32(len(buf))
			if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
				return "", err
			}
			return windows.UTF16ToString(buf[:size]), nil
		},
		version: func(exe string) string {
			out, err := exec.Command(exe, "version").Output()
			if err != nil {
				return ""
			}
			first, _, _ := strings.Cut(string(out), "\n")
			return first
		},
		wait:       30 * time.Second,
		poll:       500 * time.Millisecond,
		statusHint: "check 'Get-Service senhub-agent'",
	}.run()
}
