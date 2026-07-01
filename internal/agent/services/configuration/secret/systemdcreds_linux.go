//go:build linux

package secret

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// credsStoreSubdir is the persistent encrypted store under the config dir. Each
// secret is a `<key>.cred` file produced by `systemd-creds encrypt`. The systemd
// unit references these via one LoadCredentialEncrypted=<key>:<path> directive
// per file (directory loading is not supported before systemd 255), and at
// runtime systemd decrypts each into $CREDENTIALS_DIRECTORY/<key>.
const credsStoreSubdir = "creds.d"

// systemdCredsProvider backs ${secret:} with systemd host-key (or TPM) encrypted
// credentials. It is the coherent two-directory model:
//
//   - SEAL time (admin, root): Set encrypts the value into the persistent store
//     <configDir>/creds.d/<key>.cred. Requires root because systemd-creds reads
//     /var/lib/systemd/credential.secret.
//   - RUN time (daemon, non-root): Get reads the decrypted value systemd mounted
//     at $CREDENTIALS_DIRECTORY/<key> — readable by the service user without any
//     privilege, because systemd did the decryption before dropping to User=.
//
// Get also falls back to `systemd-creds decrypt` against the persistent store
// when $CREDENTIALS_DIRECTORY is absent (an admin running `secret get`/`key
// show` outside the unit, with root), so introspection works in both contexts.
type systemdCredsProvider struct {
	configDir string
}

func newSystemdCredsProvider(configDir string) *systemdCredsProvider {
	return &systemdCredsProvider{configDir: configDir}
}

func (p *systemdCredsProvider) storeDir() string {
	return filepath.Join(p.configDir, credsStoreSubdir)
}

func (p *systemdCredsProvider) credFile(key string) string {
	return filepath.Join(p.storeDir(), key+".cred")
}

// Get resolves a secret. At runtime under the unit it reads the decrypted
// credential from $CREDENTIALS_DIRECTORY/<key>; otherwise it decrypts the
// persistent <key>.cred with the host key (root-only admin path). Errors name
// only the secret, never its value.
func (p *systemdCredsProvider) Get(name string) (string, error) {
	key := SanitizeKey(name)

	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, key))
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("reading credential %q: %w", name, err)
		}
		// Fall through to the persistent store: the daemon may resolve a secret
		// added after the unit started (before the next restart wires it).
	}

	credPath := p.credFile(key)
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		return "", fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	bin, err := exec.LookPath("systemd-creds")
	if err != nil {
		return "", fmt.Errorf("systemd-creds not available to decrypt %q: %w", name, err)
	}
	cmd := exec.Command(bin, "decrypt", "--name="+key, credPath, "-") // #nosec G204 - key is sanitized, path is derived
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("decrypting credential %q: %w: %s", name, err, msg)
		}
		return "", fmt.Errorf("decrypting credential %q: %w", name, err)
	}
	return strings.TrimSpace(out.String()), nil
}

// Set encrypts value into <configDir>/creds.d/<key>.cred via systemd-creds. The
// plaintext is piped on STDIN so it never appears on argv or in a temp file.
// The embedded --name binds the ciphertext to the credential id the unit must
// load it under, so a mismatched LoadCredentialEncrypted= is rejected by systemd.
func (p *systemdCredsProvider) Set(name string, value Secret) error {
	bin, err := exec.LookPath("systemd-creds")
	if err != nil {
		return fmt.Errorf("systemd-creds not available (install systemd >= 250 or use the age key-file backend): %w", err)
	}
	if err := os.MkdirAll(p.storeDir(), 0o750); err != nil {
		return fmt.Errorf("creating credential store dir: %w", err)
	}
	key := SanitizeKey(name)
	out := p.credFile(key)

	cmd := exec.Command(bin, "encrypt", "--name="+key, "-", out) // #nosec G204 - key is sanitized, path is derived
	cmd.Stdin = strings.NewReader(value.Expose())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("encrypting credential %q with systemd-creds: %w: %s", name, err, msg)
		}
		return fmt.Errorf("encrypting credential %q with systemd-creds: %w", name, err)
	}
	return nil
}

// Delete removes the persistent <key>.cred. Deleting an unknown name is not an
// error. The matching unit directive is pruned the next time the unit is wired.
func (p *systemdCredsProvider) Delete(name string) error {
	key := SanitizeKey(name)
	if err := os.Remove(p.credFile(key)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting credential %q: %w", name, err)
	}
	return nil
}

// List returns the sanitized keys of the persistent store, sorted. It reads the
// store dir (not the runtime dir), so it works in the admin context where
// $CREDENTIALS_DIRECTORY is unset.
func (p *systemdCredsProvider) List() ([]string, error) {
	entries, err := os.ReadDir(p.storeDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing credentials: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); strings.HasSuffix(n, ".cred") {
			names = append(names, strings.TrimSuffix(n, ".cred"))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (p *systemdCredsProvider) Name() string { return "systemd-creds" }
