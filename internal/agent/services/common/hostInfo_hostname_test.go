package common

import (
	"runtime"
	"testing"
)

// TestNormalizeHostname pins the #627 normalization: whatever casing or
// FQDN-root decoration the OS reports, the emitted host label is the same
// lower-case form on every path (metric tags, OTLP resource, entity).
func TestNormalizeHostname(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"DASH01", "dash01"},
		{"Dash01.Example.COM", "dash01.example.com"},
		{"dash01.example.com.", "dash01.example.com"},
		{"  Web-Server-1  ", "web-server-1"},
		{"already-lower.example.com", "already-lower.example.com"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeHostname(tc.raw); got != tc.want {
			t.Errorf("normalizeHostname(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestCanonicalHostname_NonWindowsNormalizesRaw asserts the fallback path:
// without a platform FQDN source, canonicalHostname is exactly the
// normalized OS hostname. On Windows resolveHostFQDN may legitimately
// return a machine-specific FQDN, so the assertion is non-portable there.
func TestCanonicalHostname_NonWindowsNormalizesRaw(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows resolves a machine-specific FQDN; covered by TestNormalizeHostname")
	}
	if got := canonicalHostname("DASH01.Example.COM."); got != "dash01.example.com" {
		t.Errorf("canonicalHostname = %q, want dash01.example.com", got)
	}
}
