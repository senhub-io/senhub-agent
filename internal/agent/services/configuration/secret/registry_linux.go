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
// Backend selection, in precedence order (see useSystemdCreds):
//
//   - SENHUB_SECRET_BACKEND=systemd-creds|age: explicit override, wins over
//     everything (so `age` can be forced even under a unit).
//   - a populated creds.d/ store already exists: the install is on systemd-creds,
//     so every context (CLI, daemon) must agree on it.
//   - $CREDENTIALS_DIRECTORY is set AND no age store exists: a fresh install
//     under a unit that wired LoadCredentialEncrypted=; use systemd-creds. When
//     an age store DOES exist it is kept — a unit credential added for an
//     unrelated purpose must not orphan the age store.
//   - otherwise: the age key-file store (zero unit coordination, fully non-root).
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
	// Explicit override wins over everything else, including a unit-set
	// $CREDENTIALS_DIRECTORY, so an operator can always force `age` under a unit.
	switch os.Getenv(backendEnv) {
	case "systemd-creds":
		return true
	case "age":
		return false
	}

	// A populated creds.d/ store means the install is already on systemd-creds;
	// every context (CLI, daemon) must agree on it.
	if hasCredFiles(configDir) {
		return true
	}

	// $CREDENTIALS_DIRECTORY is set by systemd for ANY credential wired into the
	// unit — including one an operator added for an unrelated purpose. Do not let
	// its mere presence orphan an existing age store: only fall to systemd-creds
	// when there is no age store to keep using. A fresh install under a unit
	// (no age store, no creds.d/) still gets systemd-creds.
	if os.Getenv("CREDENTIALS_DIRECTORY") != "" {
		return !fileExists(filepath.Join(configDir, "secrets.age"))
	}
	return false
}

// hasCredFiles reports whether configDir/creds.d holds at least one *.cred file.
func hasCredFiles(configDir string) bool {
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
