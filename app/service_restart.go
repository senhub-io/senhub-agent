package app

import (
	"fmt"
	"strings"
	"time"
)

// serviceRestarter holds what restarting the installed service needs, so
// the sequence can be tested without systemd.
type serviceRestarter struct {
	installed  func() bool
	isActive   func() bool
	mainPID    func() string
	restart    func() error
	exePath    func(pid string) (string, error)
	version    func(exe string) string
	wait       time.Duration
	poll       time.Duration
	settle     time.Duration
	statusHint string
	// startIfStopped starts a service that is installed but not running
	// instead of leaving it alone.
	startIfStopped bool
}

// run restarts the service when it is installed and running, waits for a
// new process, and checks that the process executes a binary still on
// disk, i.e. the one just installed. "" means there was no running
// service to restart.
func (r serviceRestarter) run() (string, error) {
	if !r.installed() || (!r.isActive() && !r.startIfStopped) {
		return "", nil
	}
	before := r.mainPID()
	if err := r.restart(); err != nil {
		return "", fmt.Errorf("restarting the service: %w", err)
	}
	deadline := time.Now().Add(r.wait)
	pid := ""
	for {
		pid = r.mainPID()
		if r.isActive() && pid != "" && pid != "0" && pid != before {
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the service did not come back within %s; %s", r.wait, r.statusHint)
		}
		time.Sleep(r.poll)
	}
	exe, err := r.exePath(pid)
	if err != nil {
		return "", fmt.Errorf("reading the running binary of process %s: %w", pid, err)
	}
	if strings.HasSuffix(exe, " (deleted)") {
		return "", fmt.Errorf("the service restarted but runs a binary that no longer exists (%s)", exe)
	}
	if r.settle > 0 {
		settleDeadline := time.Now().Add(r.settle)
		for {
			if !r.isActive() || r.mainPID() != pid {
				return "", fmt.Errorf("the service stopped within %s of its start; %s", r.settle, r.statusHint)
			}
			if time.Now().After(settleDeadline) {
				break
			}
			time.Sleep(r.poll)
		}
	}
	line := "Service restarted"
	if v := strings.TrimSpace(r.version(exe)); v != "" {
		line += "; it now runs " + v
	}
	return line + ".", nil
}
