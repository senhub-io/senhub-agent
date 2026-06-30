package secret

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Cipher binds a secret value to the host. Implementations: an age identity held
// in a root-only key file (Linux/dev), or Windows DPAPI machine scope. The
// plaintext is handled only inside Encrypt/Decrypt and is never logged.
type Cipher interface {
	Encrypt(plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
	// Name is a short backend identifier for logging ("age-keyfile", "dpapi").
	Name() string
}

// FileStore is a Provider backed by an on-disk map of name → host-bound
// ciphertext. The file holds NO plaintext and is written 0600. The age-keyfile
// and DPAPI providers are thin wrappers around a FileStore with their own
// Cipher, so the persistence and concurrency logic lives in one place.
type FileStore struct {
	path   string
	cipher Cipher
	mu     sync.Mutex
}

// NewFileStore builds a store persisted at path, encrypting values with cipher.
func NewFileStore(path string, cipher Cipher) *FileStore {
	return &FileStore{path: path, cipher: cipher}
}

func (s *FileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading secret store %s: %w", s.path, err)
	}
	m := map[string]string{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parsing secret store %s: %w", s.path, err)
		}
	}
	return m, nil
}

// save writes the ciphertext map at 0600 via a temp file + rename so a crash
// mid-write cannot truncate the store.
func (s *FileStore) save(m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating secret store dir: %w", err)
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing secret store: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// Get decrypts and returns the value for name.
func (s *FileStore) Get(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return "", err
	}
	enc, ok := m[name]
	if !ok {
		return "", fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	ct, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("decoding secret %q: %w", name, err)
	}
	pt, err := s.cipher.Decrypt(ct)
	if err != nil {
		return "", fmt.Errorf("decrypting secret %q: %w", name, err)
	}
	return string(pt), nil
}

// Set encrypts value and stores it under name (overwrite).
func (s *FileStore) Set(name string, value Secret) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	ct, err := s.cipher.Encrypt([]byte(value.Expose()))
	if err != nil {
		return fmt.Errorf("encrypting secret %q: %w", name, err)
	}
	m[name] = base64.StdEncoding.EncodeToString(ct)
	return s.save(m)
}

// Delete removes name; deleting an unknown name is not an error.
func (s *FileStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := m[name]; !ok {
		return nil
	}
	delete(m, name)
	return s.save(m)
}

// List returns the stored names (never values), sorted.
func (s *FileStore) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// Name reports the backing cipher's identifier.
func (s *FileStore) Name() string { return s.cipher.Name() }
