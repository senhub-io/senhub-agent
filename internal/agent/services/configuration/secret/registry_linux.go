//go:build linux

package secret

import (
	"os"
	"path/filepath"
)

// backendEnv lets an operator force the systemd-creds backend in the admin
// context (e.g. `SENHUB_SECRET_BACKEND=systemd-creds agent secret migrate`),
// where $CREDENTIALS_DIRECTORY is not yet set because the seal runs outside the
// unit. Accepted values: "systemd-creds", "age".
const backendEnv = "SENHUB_SECRET_BACKEND"

// InitRegistry installs the active secret provider for Linux.
//
// systemd-creds is selected when any of these holds — otherwise the default is
// the age key-file store (zero unit coordination, fully non-root):
//
//   - $CREDENTIALS_DIRECTORY is set: the daemon is running under a unit that
//     wired LoadCredentialEncrypted=, so secrets resolve from the runtime mount.
//   - SENHUB_SECRET_BACKEND=systemd-creds: explicit admin opt-in for the seal.
//   - a populated creds.d/ store already exists: the install is on systemd-creds,
//     so every context (CLI, daemon) must agree on it.
//
// age is the default because it works unprivileged from any context and needs no
// systemd unit wiring; systemd-creds is the hardened opt-in.
func InitRegistry(configDir string) error {
	if useSystemdCreds(configDir) {
		SetProvider(newSystemdCredsProvider(configDir))
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

func useSystemdCreds(configDir string) bool {
	if os.Getenv("CREDENTIALS_DIRECTORY") != "" {
		return true
	}
	switch os.Getenv(backendEnv) {
	case "systemd-creds":
		return true
	case "age":
		return false
	}
	// Auto-detect an existing systemd-creds install: a creds.d/ holding at least
	// one .cred file.
	entries, err := os.ReadDir(filepath.Join(configDir, credsStoreSubdir))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".cred" {
			return true
		}
	}
	return false
}
