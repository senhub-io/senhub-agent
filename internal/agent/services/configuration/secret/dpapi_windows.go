//go:build windows

package secret

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/billgraziano/dpapi"
	"github.com/rs/zerolog/log"
)

// entropyLen is the size of the per-install secondary entropy. DPAPI binds
// ciphertext to the machine; the entropy additionally prevents a second install
// on the same host (or another machine-scope DPAPI caller) from decrypting this
// store. It is NOT itself a secret — losing it only forces a re-seal.
const entropyLen = 16

// entropyFile is the per-install entropy stored next to the secret store.
const entropyFile = "entropy.bin"

// dpapiStoreFile is the on-disk ciphertext map for the DPAPI backend.
const dpapiStoreFile = "secrets.dpapi"

// dpapiCipher seals values with Windows DPAPI in machine-local scope, with a
// per-install entropy mixed in. The plaintext is touched only inside
// Encrypt/Decrypt and never logged.
type dpapiCipher struct {
	entropy []byte
}

// Encrypt seals plaintext with DPAPI machine-local scope plus the install
// entropy. The error names no value.
func (c dpapiCipher) Encrypt(plaintext []byte) ([]byte, error) {
	ct, err := dpapi.EncryptBytesMachineLocalEntropy(plaintext, c.entropy)
	if err != nil {
		return nil, fmt.Errorf("dpapi encrypt: %w", err)
	}
	return ct, nil
}

// Decrypt unseals a DPAPI machine-local ciphertext. CryptUnprotectData detects
// the machine scope from the blob itself, so the decrypt counterpart of
// EncryptBytesMachineLocalEntropy is the plain entropy variant. The error names
// no value.
func (c dpapiCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	pt, err := dpapi.DecryptBytesEntropy(ciphertext, c.entropy)
	if err != nil {
		return nil, fmt.Errorf("dpapi decrypt: %w", err)
	}
	return pt, nil
}

func (c dpapiCipher) Name() string { return "dpapi" }

// NewDPAPIProvider builds the Windows DPAPI-backed secret provider rooted at
// configDir: it loads or creates the per-install entropy, then wraps a FileStore
// with the DPAPI cipher. After construction it tightens the ACLs on the store
// and entropy files to SYSTEM + Administrators only (best effort).
func NewDPAPIProvider(configDir string) (Provider, error) {
	entropy, err := loadOrCreateEntropy(configDir)
	if err != nil {
		return nil, err
	}

	storePath := filepath.Join(configDir, dpapiStoreFile)
	provider := NewFileStore(storePath, dpapiCipher{entropy: entropy})

	for _, p := range []string{storePath, filepath.Join(configDir, entropyFile)} {
		if _, statErr := os.Stat(p); statErr != nil {
			continue
		}
		if aclErr := restrictACL(p); aclErr != nil {
			log.Warn().Str("path", p).Err(aclErr).
				Msg("could not restrict DPAPI secret file ACL; relying on 0600 + parent dir")
		}
	}

	return provider, nil
}

// loadOrCreateEntropy reads <configDir>/entropy.bin, generating and persisting a
// fresh 16-byte value at 0600 when absent. The entropy is not a secret value, so
// it is safe to persist and to name in errors.
func loadOrCreateEntropy(configDir string) ([]byte, error) {
	path := filepath.Join(configDir, entropyFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != entropyLen {
			return nil, fmt.Errorf("entropy file %s has unexpected length %d (want %d)", path, len(data), entropyLen)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading entropy file %s: %w", path, err)
	}

	entropy := make([]byte, entropyLen)
	if _, err := rand.Read(entropy); err != nil {
		return nil, fmt.Errorf("generating entropy: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating config dir %s: %w", configDir, err)
	}
	if err := os.WriteFile(path, entropy, 0o600); err != nil {
		return nil, fmt.Errorf("writing entropy file %s: %w", path, err)
	}
	return entropy, nil
}

// restrictACL strips inherited ACEs and grants full control to SYSTEM and
// Administrators only, so a non-admin local user cannot read the host-bound
// ciphertext. DPAPI machine scope already lets any local process attempt
// decryption, so this file ACL is the boundary that keeps the store admin-only.
func restrictACL(path string) error {
	cmd := exec.Command("icacls", path,
		"/inheritance:r",
		"/grant:r", "SYSTEM:F",
		"/grant:r", "Administrators:F",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %s: %w: %s", path, err, string(out))
	}
	return nil
}
