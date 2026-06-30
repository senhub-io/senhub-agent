//go:build linux

package secret

import (
	"os"
	"path/filepath"
)

// InitRegistry installs the active secret provider for Linux. When systemd
// supplied a credentials directory the agent reads from it (systemd-creds);
// otherwise it falls back to an age key-file store under configDir.
func InitRegistry(configDir string) error {
	if os.Getenv("CREDENTIALS_DIRECTORY") != "" {
		SetProvider(&systemdCredsProvider{})
		return nil
	}
	p, err := NewAgeKeyfileProvider(
		filepath.Join(configDir, "agent-secret.key"),
		filepath.Join(configDir, "secrets.age"),
	)
	if err != nil {
		return err
	}
	SetProvider(p)
	return nil
}
