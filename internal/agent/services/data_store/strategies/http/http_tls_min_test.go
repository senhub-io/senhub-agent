package http

import (
	"crypto/tls"
	"testing"
)

// The configured minimum was logged and reported by the API but never
// reached the listener. This guards the mapping rather than the plumbing,
// since the plumbing is one assignment and the mapping is where a future
// enum value would silently fall back.
func TestConfiguredMinimumReachesTheListener(t *testing.T) {
	for _, c := range []struct {
		configured string
		want       uint16
	}{
		{"1.3", tls.VersionTLS13},
		{"1.2", tls.VersionTLS12},
		{"", tls.VersionTLS12},
		{"1.1", tls.VersionTLS12},
	} {
		if got := tlsVersionOf(c.configured); got != c.want {
			t.Errorf("min_tls_version %q maps to %#x, want %#x", c.configured, got, c.want)
		}
	}
}
