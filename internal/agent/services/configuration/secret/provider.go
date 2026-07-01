package secret

import (
	"fmt"
	"sort"
	"sync"
)

// Provider resolves and manages secrets in one OS-native backend
// (systemd-creds, an age key-file store, or Windows DPAPI). Implementations are
// added per OS in P1/P2; the interface is frozen here.
//
// Errors returned by any method MUST NOT contain a secret value — only the
// name, so a failure can be logged safely.
type Provider interface {
	// Get returns the plaintext for name. It returns an error that wraps
	// ErrNotFound when the name is unknown.
	Get(name string) (string, error)
	// Set stores value under name (overwrite). The value is held only for the
	// duration of the call.
	Set(name string, value Secret) error
	// Delete removes name. Deleting an unknown name is not an error.
	Delete(name string) error
	// List returns the known secret names (never values), sorted.
	List() ([]string, error)
	// Name is a short backend identifier for logging ("memory", "age-keyfile",
	// "systemd-creds", "dpapi").
	Name() string
}

// ErrNotFound is wrapped by Provider.Get when a name is unknown, so callers can
// distinguish "absent" (a default may apply) from a backend failure.
var ErrNotFound = fmt.Errorf("secret not found")

// MemoryProvider is an in-memory Provider used by tests and as a transient
// holder. It never persists to disk. Safe for concurrent use.
type MemoryProvider struct {
	mu sync.RWMutex
	m  map[string]string
}

// NewMemoryProvider builds an empty in-memory provider.
func NewMemoryProvider() *MemoryProvider { return &MemoryProvider{m: map[string]string{}} }

func (p *MemoryProvider) Get(name string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.m[name]
	if !ok {
		return "", fmt.Errorf("%q: %w", name, ErrNotFound)
	}
	return v, nil
}

func (p *MemoryProvider) Set(name string, value Secret) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[name] = value.Expose()
	return nil
}

func (p *MemoryProvider) Delete(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, name)
	return nil
}

func (p *MemoryProvider) List() ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.m))
	for k := range p.m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

func (p *MemoryProvider) Name() string { return "memory" }
