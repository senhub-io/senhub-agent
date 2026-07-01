package secret

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// ageCipher binds a secret value to the host using a single age X25519 identity
// held in a root-only key file. The identity's secret key never leaves this
// process and is never logged.
type ageCipher struct {
	identity *age.X25519Identity
}

// Encrypt seals plaintext to the identity's recipient. The age header carries no
// plaintext, so a leaked store reveals nothing without the key file.
func (c ageCipher) Encrypt(plaintext []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, c.identity.Recipient())
	if err != nil {
		return nil, fmt.Errorf("age: opening encryptor: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("age: writing ciphertext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("age: finalizing ciphertext: %w", err)
	}
	return buf.Bytes(), nil
}

// Decrypt opens ciphertext with the identity.
func (c ageCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), c.identity)
	if err != nil {
		return nil, fmt.Errorf("age: opening decryptor: %w", err)
	}
	pt, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("age: reading plaintext: %w", err)
	}
	return pt, nil
}

func (c ageCipher) Name() string { return "age-keyfile" }

// NewAgeKeyfileProvider returns a file-backed Provider whose values are sealed
// with an age X25519 identity loaded from keyPath. When keyPath is absent a new
// identity is generated and written 0600 (its parent created 0700). The key file
// holds the age secret key (Bech32 AGE-SECRET-KEY-1...) and is never logged.
func NewAgeKeyfileProvider(keyPath, storePath string) (Provider, error) {
	identity, err := loadOrCreateAgeIdentity(keyPath)
	if err != nil {
		return nil, err
	}
	return NewFileStore(storePath, ageCipher{identity: identity}), nil
}

func loadOrCreateAgeIdentity(keyPath string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(keyPath)
	if err == nil {
		identity, perr := parseAgeKeyFile(data)
		if perr != nil {
			return nil, fmt.Errorf("parsing age key file %s: %w", keyPath, perr)
		}
		return identity, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading age key file %s: %w", keyPath, err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating age identity: %w", err)
	}
	if err := writeAgeKeyFile(keyPath, identity); err != nil {
		return nil, err
	}
	return identity, nil
}

// parseAgeKeyFile reads the first non-blank, non-comment line of an age key file
// as the Bech32 secret key. The secret key is never included in any error.
func parseAgeKeyFile(data []byte) (*age.X25519Identity, error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return age.ParseX25519Identity(line)
	}
	return nil, fmt.Errorf("no age secret key found")
}

func writeAgeKeyFile(keyPath string, identity *age.X25519Identity) error {
	if dir := filepath.Dir(keyPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating age key dir: %w", err)
		}
	}
	content := "# SenHub agent secret key — keep private, never share.\n" + identity.String() + "\n"
	if err := os.WriteFile(keyPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing age key file %s: %w", keyPath, err)
	}
	return nil
}
