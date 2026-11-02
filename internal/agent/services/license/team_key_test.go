package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func teamValidator(t *testing.T, kid string, pub ed25519.PublicKey) *JWTValidator {
	t.Helper()
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewJWTValidator(publicKeyToPEM(&rootKey.PublicKey), 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.WithTeamKey(kid, pub); err != nil {
		t.Fatal(err)
	}
	return v
}

func teamClaims(sub string, lifetime time.Duration) LicenseClaims {
	now := time.Now()
	return LicenseClaims{
		Tier:             TierEnterprise,
		AuthorizedProbes: []string{"*"},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
		},
	}
}

func signEd(t *testing.T, kid string, priv ed25519.PrivateKey, claims jwt.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTeamKeyLicenceAccepted(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	v := teamValidator(t, "teams-1", pub)
	lic, err := v.ValidateLicense(signEd(t, "teams-1", priv, teamClaims("acme", 90*24*time.Hour)))
	if err != nil {
		t.Fatalf("team-signed licence refused: %v", err)
	}
	if lic.Tier != TierEnterprise || lic.Subject != "acme" || lic.IsExpired {
		t.Fatalf("unexpected licence: %+v", lic)
	}
	if !v.IsProbeAuthorized(lic, "citrix") || !VerifyBinding("", "6f1c2a3b-0000-4000-8000-000000000001", lic) {
		t.Fatal("team licence must grant paid probes fleet-wide")
	}
}

func TestTeamKeyLicenceRefused(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	v := teamValidator(t, "teams-1", pub)
	day := 24 * time.Hour
	noExp := teamClaims("acme", day)
	noExp.ExpiresAt = nil

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"unknown kid", signEd(t, "teams-9", priv, teamClaims("acme", day)), "unknown key"},
		{"no kid", signEd(t, "", priv, teamClaims("acme", day)), "unknown key"},
		{"untrusted key under a known kid", signEd(t, "teams-1", otherPriv, teamClaims("acme", day)), "signature"},
		{"lifetime above the limit", signEd(t, "teams-1", priv, teamClaims("acme", 365*day)), "limit"},
		{"no subject", signEd(t, "teams-1", priv, teamClaims("", day)), "subject"},
		{"no expiry", signEd(t, "teams-1", priv, noExp), "iat or exp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lic, err := v.ValidateLicense(c.token)
			if err == nil {
				t.Fatalf("accepted: %+v", lic)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
	if _, err := v.ValidateLicense(signEd(t, "teams-9", priv, teamClaims("acme", day))); !errors.Is(err, ErrUnknownSigningKey) {
		t.Fatalf("unknown kid must wrap ErrUnknownSigningKey, got %v", err)
	}
}

func TestTeamKeyDoesNotOpenOtherAlgorithms(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	v := teamValidator(t, "teams-1", pub)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, teamClaims("acme", time.Hour))
	tok.Header["kid"] = "teams-1"
	s, err := tok.SignedString([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateLicense(s); err == nil {
		t.Fatal("HS256 token keyed with the team public key was accepted")
	}
}

func TestWithTeamKeyRejectsBadInput(t *testing.T) {
	v := &JWTValidator{}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := v.WithTeamKey("", pub); err == nil {
		t.Fatal("empty kid accepted")
	}
	if err := v.WithTeamKey("k", pub[:16]); err == nil {
		t.Fatal("short key accepted")
	}
	if err := v.WithTeamKey("k", pub); err != nil {
		t.Fatal(err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := v.WithTeamKey("k", other); err == nil {
		t.Fatal("a trusted kid was replaced")
	}
}

func TestEmbeddedTeamKeysDecode(t *testing.T) {
	for kid, b64 := range TeamPublicKeys {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			t.Fatalf("embedded team key %q is not a base64 ed25519 key", kid)
		}
	}
	if _, err := GetDefaultValidator(7); err != nil {
		t.Fatalf("default validator: %v", err)
	}
}

func TestDefaultValidatorTrustsEmbeddedTeamKeys(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	saved := TeamPublicKeys
	TeamPublicKeys = map[string]string{"teams-test": base64.StdEncoding.EncodeToString(pub)}
	t.Cleanup(func() { TeamPublicKeys = saved })

	v, err := GetDefaultValidator(7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateLicense(signEd(t, "teams-test", priv, teamClaims("acme", 90*24*time.Hour))); err != nil {
		t.Fatalf("embedded team key not trusted: %v", err)
	}

	TeamPublicKeys = map[string]string{"bad": "!!"}
	if _, err := GetDefaultValidator(7); err == nil {
		t.Fatal("malformed embedded team key accepted")
	}
}
