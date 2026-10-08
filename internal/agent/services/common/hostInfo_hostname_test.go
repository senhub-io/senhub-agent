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

// TestHostNameOverride_AppliesToEveryOwnHostEmitter pins the one source of
// truth: metrics tags, the OTLP resource and the host entity all read the
// operator's host.name override, and all fall back to the same canonical
// name without one.
func TestHostNameOverride_AppliesToEveryOwnHostEmitter(t *testing.T) {
	t.Cleanup(func() { SetHostNameOverride("") })

	SetHostNameOverride("")
	base, err := GetHostIdentity()
	if err != nil {
		t.Skipf("host info unavailable: %v", err)
	}

	SetHostNameOverride("  preprod.example.shop ")
	id, err := GetHostIdentity()
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := GetHostResourceAttributes()
	if err != nil {
		t.Fatal(err)
	}
	tg, err := GetHostTags()
	if err != nil {
		t.Fatal(err)
	}
	if id.Name != "preprod.example.shop" || attrs["host.name"] != id.Name {
		t.Errorf("entity %q / resource %q, want the override", id.Name, attrs["host.name"])
	}
	for _, tag := range tg {
		if tag.Key == "host" && tag.Value != id.Name {
			t.Errorf("host tag = %q, want %q", tag.Value, id.Name)
		}
	}

	SetHostNameOverride("")
	again, _ := GetHostIdentity()
	attrs, _ = GetHostResourceAttributes()
	if again.Name != base.Name || attrs["host.name"] != base.Name {
		t.Errorf("without override: entity %q resource %q, want %q", again.Name, attrs["host.name"], base.Name)
	}
}
