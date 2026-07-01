//go:build linux

package secret

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SystemdCredentialDropIn returns a systemd drop-in body that wires every sealed
// secret in <configDir>/creds.d/ as an encrypted credential, one
// LoadCredentialEncrypted=<key>:<path> line per .cred file. systemd decrypts each
// into $CREDENTIALS_DIRECTORY/<key> (0400, owned by the service user) before the
// daemon starts, which is exactly where systemdCredsProvider.Get reads.
//
// Directory-form loading is not available before systemd 255, so the credentials
// must be enumerated explicitly; this drop-in is regenerated whenever the sealed
// set changes (install / refresh-unit / after sealing). It returns "" when the
// store is empty so the caller can remove a stale drop-in.
//
// The credential id equals the sanitized key, which equals the .cred stem, which
// equals the --name bound at encrypt time — so a tampered or renamed file is
// rejected by systemd rather than silently loaded under the wrong name.
func SystemdCredentialDropIn(configDir string) (string, error) {
	store := filepath.Join(configDir, credsStoreSubdir)
	entries, err := os.ReadDir(store)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading credential store %s: %w", store, err)
	}

	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); strings.HasSuffix(n, ".cred") {
			keys = append(keys, strings.TrimSuffix(n, ".cred"))
		}
	}
	if len(keys) == 0 {
		return "", nil
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# Managed by senhub-agent — do not edit.\n")
	b.WriteString("# Wires sealed secrets from creds.d/ as systemd encrypted credentials.\n")
	b.WriteString("# Regenerated on install/refresh and after sealing.\n")
	b.WriteString("[Service]\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "LoadCredentialEncrypted=%s:%s\n", k, filepath.Join(store, k+".cred"))
	}
	return b.String(), nil
}
