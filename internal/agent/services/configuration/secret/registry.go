package secret

import (
	"errors"
	"fmt"
	"sync"
)

// The registry holds the single active Provider for the process, plus the
// config directory file-backed providers use to locate their key/store. It is a
// package-level singleton initialised once at boot, before the first Resolve.
var (
	mu        sync.RWMutex
	provider  Provider
	configDir string
)

// SetConfigDir records the active configuration directory (where a file-backed
// provider keeps its key and store). Call before InitRegistry / the first
// Resolve. Recording the directory is harmless on its own — it carries no
// secret.
func SetConfigDir(dir string) {
	mu.Lock()
	configDir = dir
	mu.Unlock()
}

// ConfigDir returns the recorded configuration directory.
func ConfigDir() string {
	mu.RLock()
	defer mu.RUnlock()
	return configDir
}

// SetProvider installs the active provider. Used by the per-OS InitRegistry
// implementations (P1/P2) and by tests. Passing nil clears it (no backend).
func SetProvider(p Provider) {
	mu.Lock()
	provider = p
	mu.Unlock()
}

// ActiveProvider returns the installed provider, or nil when no secret backend
// is configured.
func ActiveProvider() Provider {
	mu.RLock()
	defer mu.RUnlock()
	return provider
}

// Resolve returns the plaintext for a ${secret:<name>} reference.
//
// A name the backend cannot resolve uses dflt when one was supplied; otherwise
// it is an error — a referenced secret that has gone missing must abort boot,
// never silently become "". When no backend is configured at all, the same rule
// applies (default, else error). The error carries only the NAME.
func Resolve(name, dflt string, hasDefault bool) (string, error) {
	p := ActiveProvider()
	if p == nil {
		if hasDefault {
			return dflt, nil
		}
		return "", fmt.Errorf("no secret backend configured to resolve ${secret:%s}", name)
	}
	v, err := p.Get(name)
	if err != nil {
		if hasDefault && errors.Is(err, ErrNotFound) {
			return dflt, nil
		}
		// The wrapped provider error names only the secret, never its value.
		return "", fmt.Errorf("resolving ${secret:%s}: %w", name, err)
	}
	return v, nil
}
