//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// serviceEnvironment returns the variables systemd gives the installed
// service, when that service runs the configuration being checked. Values
// from EnvironmentFile= override Environment=, as systemd applies them.
func serviceEnvironment(configPath string) (map[string]string, error) {
	unit, err := os.ReadFile(installedUnitPath)
	if err != nil {
		return nil, nil
	}
	execLine, _ := installedExecStart(string(unit))
	servicePath := unitConfigPath(execLine)
	if servicePath == "" || filepath.Clean(servicePath) != filepath.Clean(configPath) {
		return nil, nil
	}
	out, err := exec.Command("systemctl", "show", "senhub-agent", "-p", "Environment", "-p", "EnvironmentFiles").Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl show senhub-agent: %w", err)
	}
	env, files := parseSystemctlEnvironment(string(out))
	for _, f := range files {
		fh, err := os.Open(f.Path) // #nosec G304 - path named by the installed unit
		if err != nil {
			if f.Optional {
				continue
			}
			return env, fmt.Errorf("reading %s: %w", f.Path, err)
		}
		for k, v := range parseEnvironmentFile(fh) {
			env[k] = v
		}
		if err := fh.Close(); err != nil {
			return env, fmt.Errorf("closing %s: %w", f.Path, err)
		}
	}
	return env, nil
}
