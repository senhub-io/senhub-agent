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

// systemdCredsProvider resolves secrets from the systemd credentials directory.
//
// At runtime the agent reads decrypted credentials systemd placed in
// $CREDENTIALS_DIRECTORY (the runtime-critical Get path). Set encrypts a value
// with `systemd-creds encrypt` into a `<key>.cred` file under the same
// directory; the plaintext is piped on STDIN, never written to a temp file and
// never passed on argv.
type systemdCredsProvider struct{}

func (p *systemdCredsProvider) credentialsDir() (string, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return "", fmt.Errorf("CREDENTIALS_DIRECTORY is not set")
	}
	return dir, nil
}

// Get reads $CREDENTIALS_DIRECTORY/<key>, trims surrounding whitespace and
// returns it. An absent credential wraps ErrNotFound. Errors name only the
// secret, never its value.
func (p *systemdCredsProvider) Get(name string) (string, error) {
	dir, err := p.credentialsDir()
	if err != nil {
		return "", err
	}
	key := SanitizeKey(name)
	data, err := os.ReadFile(filepath.Join(dir, key))
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading credential %q: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Set encrypts value with systemd-creds and writes <key>.cred. The plaintext is
// supplied on STDIN so it never appears on argv or in a temp file.
func (p *systemdCredsProvider) Set(name string, value Secret) error {
	dir, err := p.credentialsDir()
	if err != nil {
		return err
	}
	bin, err := exec.LookPath("systemd-creds")
	if err != nil {
		return fmt.Errorf("systemd-creds not available (install systemd or use the age key-file backend): %w", err)
	}
	key := SanitizeKey(name)
	outPath := filepath.Join(dir, key+".cred")

	cmd := exec.Command(bin, "encrypt", "--name="+key, "-", outPath)
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

// Delete removes the <key>.cred file. Deleting an unknown name is not an error.
func (p *systemdCredsProvider) Delete(name string) error {
	dir, err := p.credentialsDir()
	if err != nil {
		return err
	}
	key := SanitizeKey(name)
	if err := os.Remove(filepath.Join(dir, key+".cred")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting credential %q: %w", name, err)
	}
	return nil
}

// List returns the sanitized keys of the .cred files present, sorted.
func (p *systemdCredsProvider) List() ([]string, error) {
	dir, err := p.credentialsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing credentials: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".cred") {
			names = append(names, strings.TrimSuffix(n, ".cred"))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (p *systemdCredsProvider) Name() string { return "systemd-creds" }
