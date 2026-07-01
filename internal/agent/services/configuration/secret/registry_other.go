//go:build !linux && !windows

package secret

import "path/filepath"

// InitRegistry installs the age key-file provider on non-Linux, non-Windows
// platforms (darwin and other dev builds) so the secret scheme works there too.
func InitRegistry(configDir string) error {
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
