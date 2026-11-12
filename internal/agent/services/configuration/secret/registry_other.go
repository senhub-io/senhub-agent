//go:build !linux && !windows

package secret

import (
	"path/filepath"
)

// InitRegistry installs the age key-file provider on non-Linux, non-Windows
// platforms (darwin and other dev builds) so the secret scheme works there too.
func InitRegistry(configDir string) error {
	keyPath, storePath, err := ageStorePaths(configDir)
	if err != nil {
		return err
	}
	setStoreDir(filepath.Dir(keyPath))
	p, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		return err
	}
	SetProvider(p)
	return nil
}
