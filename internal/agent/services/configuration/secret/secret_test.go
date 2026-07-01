package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const plain = "sup3r-s3cr3t-v@lue"

// TestSecret_NeverLeaks is the security-critical test: the wrapped value must
// not appear through ANY stringification path.
func TestSecret_NeverLeaks(t *testing.T) {
	s := New(plain)

	renders := map[string]string{
		"%v":           fmt.Sprintf("%v", s),
		"%s":           fmt.Sprintf("%s", s),
		"%q":           fmt.Sprintf("%q", s),
		"%+v":          fmt.Sprintf("%+v", s),
		"%#v":          fmt.Sprintf("%#v", s),
		"%d-on-struct": fmt.Sprintf("%v", struct{ S Secret }{s}),
		"String()":     s.String(),
	}
	for what, got := range renders {
		if strings.Contains(got, plain) {
			t.Errorf("%s leaked the secret value: %q", what, got)
		}
		if !strings.Contains(got, "****") {
			t.Errorf("%s did not show the redaction marker: %q", what, got)
		}
	}

	if b, _ := json.Marshal(s); strings.Contains(string(b), plain) {
		t.Errorf("MarshalJSON leaked: %s", b)
	}
	if b, _ := yaml.Marshal(s); strings.Contains(string(b), plain) {
		t.Errorf("MarshalYAML leaked: %s", b)
	}
	if b, _ := json.Marshal(map[string]Secret{"password": s}); strings.Contains(string(b), plain) {
		t.Errorf("nested-in-map JSON leaked: %s", b)
	}

	// Expose is the one deliberate reveal boundary.
	if s.Expose() != plain {
		t.Errorf("Expose() = %q, want the original value", s.Expose())
	}
}

func TestResolve(t *testing.T) {
	t.Cleanup(func() { SetProvider(nil) })

	// No backend, no default → error (never silently empty).
	SetProvider(nil)
	if _, err := Resolve("veeam.password", "", false); err == nil {
		t.Error("no backend + no default: expected error")
	}
	// No backend, with default → default.
	if v, err := Resolve("veeam.password", "fallback", true); err != nil || v != "fallback" {
		t.Errorf("no backend + default: got %q, %v", v, err)
	}

	mp := NewMemoryProvider()
	_ = mp.Set("veeam.password", New(plain))
	SetProvider(mp)

	if v, err := Resolve("veeam.password", "", false); err != nil || v != plain {
		t.Errorf("present secret: got %q, %v", v, err)
	}
	// Missing name with default → default.
	if v, err := Resolve("absent", "dflt", true); err != nil || v != "dflt" {
		t.Errorf("missing + default: got %q, %v", v, err)
	}
	// Missing name without default → error that names the key but not a value.
	_, err := Resolve("absent", "", false)
	if err == nil {
		t.Fatal("missing + no default: expected error")
	}
	if !strings.Contains(err.Error(), "absent") || strings.Contains(err.Error(), plain) {
		t.Errorf("error should name the key, never a value: %v", err)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound: %v", err)
	}
}

func TestSanitizeKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"veeam-prod.password", "veeam-prod.password"},
		{"pg.billing.password", "pg.billing.password"},
		{"citrix-1.director.auth.password", "citrix-1.director.auth.password"},
	}
	for _, c := range cases {
		if got := SanitizeKey(c.in); got != c.want {
			t.Errorf("SanitizeKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Illegal chars are replaced and a hash is appended (lossy → unique).
	a := SanitizeKey("pg billing@dc1.password")
	if strings.ContainsAny(a, " @") {
		t.Errorf("illegal chars survived: %q", a)
	}
	// Distinct lossy inputs must not collide.
	b := SanitizeKey("pg-billing-dc1.password")
	if a == b {
		t.Errorf("distinct inputs collided: %q == %q", a, b)
	}
	// Over-length is capped.
	long := SanitizeKey(strings.Repeat("x", 200) + ".password")
	if len(long) > maxKeyLen {
		t.Errorf("key not capped: len=%d", len(long))
	}
}

func TestFindInlineSecrets(t *testing.T) {
	params := map[string]interface{}{
		"endpoint": "https://host:9419",
		"username": "svc",                 // identifier, NOT a secret → skip
		"password": "h3llo",               // flat secret
		"verify":   "${env:VERIFY}",       // already a reference → skip
		"token":    "${secret:foo.token}", // already a secret ref → skip
		"director": map[string]interface{}{ // nested
			"auth": map[string]interface{}{"password": "nested-pw"},
		},
		"v3": map[string]interface{}{
			"users": []interface{}{ // indexed
				map[string]interface{}{"auth_password": "u0-pw"},
				map[string]interface{}{"name": "u1"}, // no secret
			},
		},
	}
	found := FindInlineSecrets("acme", params)

	keys := map[string]string{}
	for _, s := range found {
		keys[s.Key()] = s.Value
		if s.InstanceName != "acme" {
			t.Errorf("wrong instance: %q", s.InstanceName)
		}
	}
	want := map[string]string{
		"acme.password":                 "h3llo",
		"acme.director.auth.password":   "nested-pw",
		"acme.v3.users.0.auth_password": "u0-pw",
	}
	if len(keys) != len(want) {
		t.Fatalf("found %d secrets, want %d: %v", len(keys), len(want), keys)
	}
	for k, v := range want {
		if keys[k] != v {
			t.Errorf("key %q: got %q, want %q", k, keys[k], v)
		}
	}
}
