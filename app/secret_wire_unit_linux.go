//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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

	body, err := secret.SystemdCredentialDropIn(configDir)
	if err != nil {
		return fmt.Errorf("generating credentials drop-in: %w", err)
	}

	if strings.TrimSpace(body) == "" {
		// No sealed secrets: remove a stale drop-in so the unit stops trying to
		// load credentials that no longer exist.
		if rmErr := os.Remove(credentialsDropInPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return fmt.Errorf("removing stale drop-in: %w", rmErr)
		}
		if err := daemonReload(); err != nil {
			return err
		}
		fmt.Printf("No sealed secrets in %s/creds.d — removed any credentials drop-in.\n", configDir)
		return nil
	}

	if err := os.MkdirAll(credentialsDropInDir, 0o755); err != nil {
		return fmt.Errorf("creating drop-in dir: %w", err)
	}
	if err := os.WriteFile(credentialsDropInPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing drop-in: %w", err)
	}
	if err := daemonReload(); err != nil {
		return err
	}

	n := strings.Count(body, "LoadCredentialEncrypted=")
	fmt.Printf("Wired %d sealed secret(s) into %s\n", n, credentialsDropInPath)
	fmt.Println("Run 'senhub-agent restart' to load the credentials into the running service.")
	return nil
}

func daemonReload() error {
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
