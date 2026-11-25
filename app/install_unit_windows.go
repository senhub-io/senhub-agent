//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/svc/mgr"
)

// currentExecutable resolves the running binary path so that an ImagePath
// registered through a symlink or a subst drive compares equal.
func currentExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return exe
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		return resolved
	}
	return exe
}

func openAgentService() (*mgr.Mgr, *mgr.Service, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the service manager: %w", err)
	}
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		m.Disconnect() //nolint:errcheck // best-effort release
		return nil, nil, fmt.Errorf("opening service %s: %w", windowsServiceName, err)
	}
	return m, s, nil
}

func serviceEnabledOnHost() bool {
	m, s, err := openAgentService()
	if err != nil {
		return false
	}
	defer m.Disconnect() //nolint:errcheck // best-effort release
	defer s.Close()      //nolint:errcheck // best-effort release
	cfg, err := s.Config()
	if err != nil {
		return false
	}
	return cfg.StartType == mgr.StartAutomatic
}

func installUnitCurrent(serviceUser string, serviceArgs []string) bool {
	m, s, err := openAgentService()
	if err != nil {
		return false
	}
	defer m.Disconnect() //nolint:errcheck // best-effort release
	defer s.Close()      //nolint:errcheck // best-effort release
	cfg, err := s.Config()
	if err != nil {
		return false
	}
	// The MSI owns the command line of the service it installed.
	if !hostMsiManaged() && !serviceCommandLineMatches(cfg.BinaryPathName, currentExecutable(), serviceArgs, syscall.EscapeArg) {
		return false
	}
	have, err := readRecovery(s)
	if err != nil {
		return false
	}
	return !recoveryDrift(have, desiredRecovery())
}

// reconcileInstalledServiceOnHost repairs the registration in place: a
// drifted ImagePath or a disabled start is written back, and the recovery
// policy is enforced. Nothing is restarted.
func reconcileInstalledServiceOnHost(serviceUser string, serviceArgs []string) ([]string, error) {
	m, s, err := openAgentService()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect() //nolint:errcheck // best-effort release
	defer s.Close()      //nolint:errcheck // best-effort release

	cfg, err := s.Config()
	if err != nil {
		return nil, fmt.Errorf("reading service config: %w", err)
	}

	changed := false
	// The MSI owns the command line of the service it installed: there only
	// the start type and the recovery policy are enforced.
	if !hostMsiManaged() && !serviceCommandLineMatches(cfg.BinaryPathName, currentExecutable(), serviceArgs, syscall.EscapeArg) ||
		cfg.StartType != mgr.StartAutomatic {
		updated := cfg
		if !hostMsiManaged() {
			updated.BinaryPathName = serviceCommandLine(currentExecutable(), serviceArgs, syscall.EscapeArg)
		}
		updated.StartType = mgr.StartAutomatic
		if err := s.UpdateConfig(updated); err != nil {
			return nil, fmt.Errorf("updating service config: %w", err)
		}
		changed = true
	}

	recoveryChanged, err := applyServiceRecovery(s)
	if err != nil {
		return nil, fmt.Errorf("applying service recovery: %w", err)
	}

	if changed || recoveryChanged {
		return []string{"service " + windowsServiceName}, nil
	}
	return nil, nil
}

func applyRecoveryAfterInstall() error {
	m, s, err := openAgentService()
	if err != nil {
		return err
	}
	defer m.Disconnect() //nolint:errcheck // best-effort release
	defer s.Close()      //nolint:errcheck // best-effort release
	if _, err := applyServiceRecovery(s); err != nil {
		return fmt.Errorf("applying service recovery: %w", err)
	}
	return nil
}
