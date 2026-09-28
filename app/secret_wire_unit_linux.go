//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration/secret"
)

// credentialsDropInPath is the systemd drop-in that wires the sealed secrets in
// creds.d/ as encrypted credentials. A drop-in (not the main unit) keeps the
// generated LoadCredentialEncrypted= lines separate from the hardened unit that
// install/refresh-unit own.
const credentialsDropInDir = "/etc/systemd/system/senhub-agent.service.d"
const credentialsDropInPath = credentialsDropInDir + "/10-senhub-credentials.conf"

// wireSystemdUnit regenerates the credentials drop-in from <configDir>/creds.d/
// and runs daemon-reload. It is the explicit operator step after sealing with
// the systemd-creds backend: it does NOT restart the running service (that is
// the operator's call), it only makes the new unit definition available.
// Writing under /etc/systemd and daemon-reload need root.
func wireSystemdUnit(configDir string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("wire-unit writes %s and runs daemon-reload — run as root (sudo)", credentialsDropInPath)
	}

	n, _, err := syncCredentialsDropIn(configDir)
	if err != nil {
		return err
	}
	if err := daemonReload(); err != nil {
		return err
	}
	if n == 0 {
		fmt.Printf("No sealed secrets in %s/creds.d — removed any credentials drop-in.\n", configDir)
		return nil
	}
	fmt.Printf("Wired %d sealed secret(s) into %s\n", n, credentialsDropInPath)
	fmt.Println("Run 'senhub-agent restart' to load the credentials into the running service.")
	return nil
}

// syncCredentialsDropIn brings the drop-in in line with <configDir>/creds.d/:
// written when the store holds credentials, removed when it holds none. It
// returns how many credentials the drop-in wires and whether the file changed,
// so a caller that only follows the store reloads systemd when it must.
func syncCredentialsDropIn(configDir string) (int, bool, error) {
	body, err := secret.SystemdCredentialDropIn(configDir)
	if err != nil {
		return 0, false, fmt.Errorf("generating credentials drop-in: %w", err)
	}
	changed, err := syncDropIn(credentialsDropInPath, body)
	if err != nil {
		return 0, false, err
	}
	return strings.Count(body, "LoadCredentialEncrypted="), changed, nil
}

// followCredentialStore is what install and refresh-unit run after writing
// the unit: the drop-in follows creds.d/ without a separate wire-unit step.
// A failure is reported, not fatal: the unit itself is already in place and
// wire-unit remains the manual repair.
func followCredentialStore(configPath string) {
	if configPath == "" {
		resolved, err := cliArgs.GetAbsoluteConfigPath("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: credentials drop-in not checked: resolving config path: %v\n", err)
			return
		}
		configPath = resolved
	}
	configDir := filepath.Dir(configPath)

	n, changed, err := syncCredentialsDropIn(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: credentials drop-in not updated: %v; run 'senhub-agent secret wire-unit'\n", err)
		return
	}
	if !changed {
		return
	}
	if err := daemonReload(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		return
	}
	if n == 0 {
		fmt.Printf("Removed the credentials drop-in: %s/creds.d holds no sealed secret.\n", configDir)
		return
	}
	fmt.Printf("Wired %d sealed secret(s) from %s/creds.d into %s\n", n, configDir, credentialsDropInPath)
}

// removeCredentialsDropIn deletes the drop-in, and its directory when
// nothing else lives there: an operator's own drop-ins stay.
func removeCredentialsDropIn() error {
	if err := os.Remove(credentialsDropInPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing credentials drop-in %s: %w", credentialsDropInPath, err)
	}
	if entries, err := os.ReadDir(credentialsDropInDir); err == nil && len(entries) == 0 {
		if err := os.Remove(credentialsDropInDir); err != nil {
			return fmt.Errorf("removing empty drop-in dir %s: %w", credentialsDropInDir, err)
		}
	}
	return nil
}

func daemonReload() error {
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
