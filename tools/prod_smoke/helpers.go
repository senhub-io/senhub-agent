//go:build prod_smoke

package prod_smoke

import (
	"os"
	"strings"
)

// envOr returns the value of the env var or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// homeDir returns the user's home, accepting either HOME (Unix) or
// USERPROFILE (Windows; unlikely needed here but cheap).
func homeDir() (string, error) {
	if v := envOr("HOME", ""); v != "" {
		return v, nil
	}
	if v := envOr("USERPROFILE", ""); v != "" {
		return v, nil
	}
	return os.UserHomeDir()
}

// expandHome rewrites a leading "~/" to the user's home directory.
// Returns the original string when no expansion is needed (or the
// home dir cannot be resolved).
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := homeDir()
	if err != nil {
		return p
	}
	return home + p[1:]
}
