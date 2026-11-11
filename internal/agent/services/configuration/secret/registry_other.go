//go:build !linux && !windows

package secret

import (
	"os"
	"path/filepath"

	"senhub-agent.go/internal/agent/services/instanceid"
)

// InitRegistry installs the age key-file provider on non-Linux, non-Windows
// platforms (darwin and other dev builds) so the secret scheme works there too.
func InitRegistry(configDir string) error {
	keyPath := filepath.Join(configDir, "agent-secret.key")
	storePath := filepath.Join(configDir, "secrets.age")
	// A read-only configuration directory cannot hold the age key and store:
	// fall back to the agent state directory unless those files are already
	// in the configuration directory.
	if !fileExists(keyPath) && !fileExists(storePath) && !dirWritable(configDir) {
		stateDir := instanceid.OwnStateDir()
		if err := os.MkdirAll(stateDir, 0700); err != nil {
			return err
		}
		keyPath = filepath.Join(stateDir, "agent-secret.key")
		storePath = filepath.Join(stateDir, "secrets.age")
	}
	setStoreDir(filepath.Dir(keyPath))
	p, err := NewAgeKeyfileProvider(keyPath, storePath)
	if err != nil {
		return err
	}
	SetProvider(p)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
