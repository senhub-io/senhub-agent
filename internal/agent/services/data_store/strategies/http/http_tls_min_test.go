package http

import (
	"crypto/tls"
	"path/filepath"
	"strings"
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

// A relative certificate path resolves against the unit's working
// directory, not against the configuration directory, which is how
// "tls.enabled: true" with no explicit paths became a silent outage: the
// log named ./certs/agent-cert.pem and the operator looked for it beside
// agent.yaml. The log must name the file the process would actually open.
func TestRelativeCertificatePathIsReportedAbsolute(t *testing.T) {
	got := absolutePathOf("./certs/agent-cert.pem")
	if !filepath.IsAbs(got) {
		t.Errorf("relative path reported as %q, want an absolute path", got)
	}
	if strings.HasPrefix(got, ".") {
		t.Errorf("path %q still reads as relative", got)
	}

	abs := filepath.Join(string(filepath.Separator), "etc", "senhub-agent", "certs", "agent-cert.pem")
	if got := absolutePathOf(abs); got != abs {
		t.Errorf("absolute path rewritten to %q, want %q unchanged", got, abs)
	}

	if got := absolutePathOf(""); got != "" {
		t.Errorf("empty path became %q, want it left empty so the caller can tell it was unset", got)
	}
}
