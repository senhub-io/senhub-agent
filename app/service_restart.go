package app

import (
	"fmt"
	"strings"
	"time"
)

// serviceRestarter holds what restarting the installed service needs, so
// the sequence can be tested without systemd.
type serviceRestarter struct {
	installed func() bool
	isActive  func() bool
	mainPID   func() string
	restart   func() error
	exePath   func(pid string) (string, error)
	version   func(exe string) string
	wait      time.Duration
	poll      time.Duration
}

// run restarts the service when it is installed and running, waits for a
// new process, and checks that the process executes a binary still on
// disk, i.e. the one just installed. "" means there was no running
// service to restart.
func (r serviceRestarter) run() (string, error) {
	if !r.installed() || !r.isActive() {
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
			return "", fmt.Errorf("the service did not come back within %s; check 'systemctl status senhub-agent'", r.wait)
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
	line := "Service restarted"
	if v := strings.TrimSpace(r.version(exe)); v != "" {
		line += "; it now runs " + v
	}
	return line + ".", nil
}
