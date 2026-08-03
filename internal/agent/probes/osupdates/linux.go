// Linux backend logic for the os_updates probe. This file carries no
// build tag on purpose: every parser and the backend selection are pure
// functions over injected seams (command runner, PATH lookup, file
// existence), so the unit tests run on every development platform.
// The //go:build linux selector lives in collector_linux.go.

package osupdates

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/logger"
)

const (
	aptCheckPath       = "/usr/lib/update-notifier/apt-check"
	rebootRequiredFile = "/var/run/reboot-required"
)

// runCommandFunc runs a command and returns its combined stdout+stderr.
// Combined because apt-check writes its "updates;security" result to
// stderr. Exit codes travel inside the error (see exitCode).
type runCommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// exitCode extracts the process exit code from a runCommandFunc error;
// -1 when the error carries none (start failure, context timeout).
func exitCode(err error) int {
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}
	return -1
}

// linuxCollector queries the detected package manager, read-only and
// without privilege escalation.
type linuxCollector struct {
	run        runCommandFunc
	lookPath   func(string) (string, error)
	fileExists func(string) bool
	logger     *logger.ModuleLogger
}

func newLinuxCollector(moduleLogger *logger.ModuleLogger) *linuxCollector {
	return &linuxCollector{
		run:      runCommand,
		lookPath: exec.LookPath,
		fileExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		logger: moduleLogger,
	}
}

func (c *linuxCollector) collect(ctx context.Context) (updatesStatus, error) {
	switch {
	case c.hasCommand("apt-get"):
		return c.collectApt(ctx)
	case c.hasCommand("dnf"):
		return c.collectDnf(ctx, "dnf")
	case c.hasCommand("yum"):
		return c.collectDnf(ctx, "yum")
	}
	return updatesStatus{}, errors.New("no supported package manager found (apt-get, dnf, yum)")
}

func (c *linuxCollector) hasCommand(name string) bool {
	_, err := c.lookPath(name)
	return err == nil
}

// collectApt prefers apt-check (exact security count maintained by the
// distribution) and falls back to parsing an apt-get upgrade simulation.
func (c *linuxCollector) collectApt(ctx context.Context) (updatesStatus, error) {
	status := updatesStatus{packageManager: "apt"}

	counted := false
	if c.fileExists(aptCheckPath) {
		out, err := c.run(ctx, aptCheckPath)
		if err != nil {
			c.logger.Warn().Err(err).Msg("apt-check failed; falling back to apt-get -s upgrade")
		} else {
			pending, security, perr := parseAptCheck(string(out))
			if perr != nil {
				c.logger.Warn().Err(perr).Msg("apt-check output unparseable; falling back to apt-get -s upgrade")
			} else {
				status.pending, status.pendingSecurity = pending, security
				counted = true
			}
		}
	}
	if !counted {
		out, err := c.run(ctx, "apt-get", "-s", "upgrade")
		if err != nil {
			return updatesStatus{}, fmt.Errorf("apt-get -s upgrade: %w", err)
		}
		status.pending, status.pendingSecurity = parseAptSimulation(string(out))
	}

	status.rebootRequired = c.fileExists(rebootRequiredFile)
	return status, nil
}

// parseAptCheck parses the "pending;security" pair apt-check prints on
// stderr, e.g. "42;7".
func parseAptCheck(out string) (int, int, error) {
	trimmed := strings.TrimSpace(out)
	parts := strings.Split(trimmed, ";")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("apt-check: expected \"updates;security\", got %q", trimmed)
	}
	pending, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("apt-check: parsing update count: %w", err)
	}
	security, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("apt-check: parsing security count: %w", err)
	}
	return pending, security, nil
}

// parseAptSimulation counts "Inst" lines from `apt-get -s upgrade`
// output. A line is a security update when its archive origin names a
// security suite, e.g.:
//
//	Inst libssl3 [3.0.11-1~deb12u2] (3.0.13-1~deb12u1 Debian-Security:12/stable-security [amd64])
//	Inst curl [7.81.0-1ubuntu1.15] (7.81.0-1ubuntu1.16 Ubuntu:22.04/jammy-security [amd64])
func parseAptSimulation(out string) (pending, security int) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		pending++
		if strings.Contains(strings.ToLower(line), "-security") {
			security++
		}
	}
	return pending, security
}

// collectDnf covers dnf and yum (same CLI surface for updateinfo).
func (c *linuxCollector) collectDnf(ctx context.Context, tool string) (updatesStatus, error) {
	status := updatesStatus{packageManager: tool}

	out, err := c.run(ctx, tool, "-q", "updateinfo", "list")
	if err != nil {
		return updatesStatus{}, fmt.Errorf("%s -q updateinfo list: %w", tool, err)
	}
	status.pending = countAdvisoryLines(string(out))

	secOut, err := c.run(ctx, tool, "-q", "updateinfo", "list", "--security")
	if err != nil {
		c.logger.Warn().Err(err).Str("tool", tool).
			Msg("security advisory count unavailable; reporting 0 pending security updates")
	} else {
		status.pendingSecurity = countAdvisoryLines(string(secOut))
	}

	status.rebootRequired = c.dnfRebootRequired(ctx)
	return status, nil
}

// countAdvisoryLines counts advisory lines in `dnf -q updateinfo list`
// output. Each advisory line has at least 3 columns:
//
//	RHSA-2026:0123 Important/Sec. openssl-libs-1:3.0.7-27.el9_3.x86_64
//
// Header and noise lines (subscription-manager banner, metadata
// timestamp, the dnf5 column header) are skipped.
func countAdvisoryLines(out string) int {
	count := 0
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" ||
			strings.HasPrefix(line, "Last metadata expiration") ||
			strings.HasPrefix(line, "Updating Subscription Management") ||
			strings.HasPrefix(line, "Name ") {
			continue
		}
		if len(strings.Fields(line)) < 3 {
			continue
		}
		count++
	}
	return count
}

// dnfRebootRequired interprets `needs-restarting -r`: exit 0 = no
// reboot needed, exit 1 = reboot needed. A missing tool or any other
// outcome is best-effort false.
func (c *linuxCollector) dnfRebootRequired(ctx context.Context) bool {
	if !c.hasCommand("needs-restarting") {
		c.logger.Debug().Msg("needs-restarting not found; reboot-required flag reported as 0")
		return false
	}
	_, err := c.run(ctx, "needs-restarting", "-r")
	if err == nil {
		return false
	}
	if exitCode(err) == 1 {
		return true
	}
	c.logger.Warn().Err(err).Msg("needs-restarting -r failed; reboot-required flag reported as 0")
	return false
}
