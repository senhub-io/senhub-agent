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

func generateRootKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	return key
}

func generatePlatformKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate platform key: %v", err)
	}
	return pub, priv
}

func newTestValidator(t *testing.T, root *rsa.PrivateKey) *JWTValidator {
	t.Helper()
	v, err := NewJWTValidator(publicKeyToPEM(&root.PublicKey), 7)
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}
	return v
}

func signDelegation(t *testing.T, root *rsa.PrivateKey, claims DelegationClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["typ"] = DelegationType
	signed, err := token.SignedString(root)
	if err != nil {
		t.Fatalf("sign delegation: %v", err)
	}
	return signed
}

func signLicence(t *testing.T, platformPriv ed25519.PrivateKey, kid, dlg string, claims LicenseClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = kid
	if dlg != "" {
		token.Header["dlg"] = dlg
	}
	signed, err := token.SignedString(platformPriv)
	if err != nil {
		t.Fatalf("sign licence: %v", err)
	}
	return signed
}

func defaultDelegationClaims(now time.Time, pub ed25519.PublicKey) DelegationClaims {
	return DelegationClaims{
		Platform: "platform-a",
		KID:      "platform-a",
		Pub:      base64.StdEncoding.EncodeToString(pub),
		Scope: DelegationScope{
			Subjects:        []string{"client1"},
			SubjectPrefixes: []string{"fleet-"},
		},
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now.Add(-24 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(31 * 24 * time.Hour)),
		},
	}
}

func defaultLicenceClaims(now time.Time, subject string) LicenseClaims {
	return LicenseClaims{
		Tier:             TierEnterprise,
		AuthorizedProbes: []string{"*"},
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * 24 * time.Hour)),
		},
	}
}

func TestDelegatedLicence(t *testing.T) {
	now := time.Now()

	t.Run("valid delegated licence accepted", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))

		got, err := v.ValidateLicense(lic)
		if err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
		if got.Tier != TierEnterprise {
			t.Errorf("Tier = %q, want %q", got.Tier, TierEnterprise)
		}
		if got.Subject != "client1" {
			t.Errorf("Subject = %q, want client1", got.Subject)
		}
		if got.Platform != "platform-a" {
			t.Errorf("Platform = %q, want platform-a", got.Platform)
		}
		wantDlgExp := now.Add(31 * 24 * time.Hour).Truncate(time.Second)
		if !got.DelegationExpiresAt.Equal(wantDlgExp) {
			t.Errorf("DelegationExpiresAt = %v, want %v", got.DelegationExpiresAt, wantDlgExp)
		}
		if !v.IsProbeAuthorized(got, "citrix") {
			t.Error("delegated Enterprise licence must authorize citrix")
		}
	})

	t.Run("subject accepted by exact subjects list", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		if _, err := v.ValidateLicense(lic); err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
	})

	t.Run("subject accepted by prefix", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "fleet-eu-01"))
		if _, err := v.ValidateLicense(lic); err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
	})

	t.Run("delegation absent", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		_, priv := generatePlatformKey(t)
		lic := signLicence(t, priv, "platform-a", "", defaultLicenceClaims(now, "client1"))
		_, err := v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationMissing) {
			t.Fatalf("err = %v, want ErrDelegationMissing", err)
		}
	})

	t.Run("delegation signed by another RSA key", func(t *testing.T) {
		root := generateRootKey(t)
		other := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, other, defaultDelegationClaims(now, pub))
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		_, err := v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("delegation with typ missing", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, defaultDelegationClaims(now, pub))
		dlg, err := token.SignedString(root)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		_, err = v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("delegation kid differs from licence kid", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		claims := defaultDelegationClaims(now, pub)
		claims.KID = "other-platform"
		dlg := signDelegation(t, root, claims)
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		_, err := v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("licence signed by a different ed25519 key", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, _ := generatePlatformKey(t)
		_, otherPriv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		lic := signLicence(t, otherPriv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		if _, err := v.ValidateLicense(lic); err == nil {
			t.Fatal("ValidateLicense succeeded, want signature error")
		}
	})

	t.Run("expired delegation, licence in grace", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlgClaims := defaultDelegationClaims(now, pub)
		dlgClaims.IssuedAt = jwt.NewNumericDate(now.Add(-40 * 24 * time.Hour))
		dlgClaims.ExpiresAt = jwt.NewNumericDate(now.Add(-3 * 24 * time.Hour))
		dlg := signDelegation(t, root, dlgClaims)
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.IssuedAt = jwt.NewNumericDate(now.Add(-10 * 24 * time.Hour))
		licClaims.ExpiresAt = jwt.NewNumericDate(now.Add(-3 * 24 * time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)

		got, err := v.ValidateLicense(lic)
		if err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
		if !got.IsExpired {
			t.Error("IsExpired = false, want true")
		}
		if !v.IsInGracePeriod(got) {
			t.Error("IsInGracePeriod = false, want true (now < dlg.exp+7d)")
		}
	})

	t.Run("expired delegation, licence out of grace", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlgClaims := defaultDelegationClaims(now, pub)
		dlgClaims.IssuedAt = jwt.NewNumericDate(now.Add(-40 * 24 * time.Hour))
		dlgClaims.ExpiresAt = jwt.NewNumericDate(now.Add(-10 * 24 * time.Hour))
		dlg := signDelegation(t, root, dlgClaims)
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.IssuedAt = jwt.NewNumericDate(now.Add(-35 * 24 * time.Hour))
		licClaims.ExpiresAt = jwt.NewNumericDate(now.Add(-10 * 24 * time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)

		got, err := v.ValidateLicense(lic)
		if err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
		if !got.IsExpired {
			t.Error("IsExpired = false, want true")
		}
		if v.IsInGracePeriod(got) {
			t.Error("IsInGracePeriod = true, want false (beyond dlg.exp+7d)")
		}
	})

	t.Run("licence exp after dlg exp refused", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.ExpiresAt = jwt.NewNumericDate(now.Add(32 * 24 * time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)
		_, err := v.ValidateLicense(lic)
		if err == nil || !strings.Contains(err.Error(), "outlives") {
			t.Fatalf("err = %v, want outlives error", err)
		}
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("licence iat before dlg iat refused", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.IssuedAt = jwt.NewNumericDate(now.Add(-48 * time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)
		_, err := v.ValidateLicense(lic)
		if err == nil || !strings.Contains(err.Error(), "outside the delegation") {
			t.Fatalf("err = %v, want outside-the-delegation error", err)
		}
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("subject outside scope", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "other-customer"))
		_, err := v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationScope) {
			t.Fatalf("err = %v, want ErrDelegationScope", err)
		}
	})

	t.Run("delegation with empty scope", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		claims := defaultDelegationClaims(now, pub)
		claims.Scope = DelegationScope{}
		dlg := signDelegation(t, root, claims)
		lic := signLicence(t, priv, "platform-a", dlg, defaultLicenceClaims(now, "client1"))
		_, err := v.ValidateLicense(lic)
		if !errors.Is(err, ErrDelegationInvalid) {
			t.Fatalf("err = %v, want ErrDelegationInvalid", err)
		}
	})

	t.Run("no subject", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		licClaims := defaultLicenceClaims(now, "")
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)
		if _, err := v.ValidateLicense(lic); err == nil {
			t.Fatal("ValidateLicense succeeded, want error")
		}
	})

	t.Run("forward-dated iat", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.IssuedAt = jwt.NewNumericDate(now.Add(time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)
		if _, err := v.ValidateLicense(lic); err == nil {
			t.Fatal("ValidateLicense succeeded, want error")
		}
	})

	t.Run("lifetime above 92 days", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, priv := generatePlatformKey(t)
		dlgClaims := defaultDelegationClaims(now, pub)
		dlgClaims.ExpiresAt = jwt.NewNumericDate(now.Add(200 * 24 * time.Hour))
		dlg := signDelegation(t, root, dlgClaims)
		licClaims := defaultLicenceClaims(now, "client1")
		licClaims.ExpiresAt = jwt.NewNumericDate(now.Add(93 * 24 * time.Hour))
		lic := signLicence(t, priv, "platform-a", dlg, licClaims)
		if _, err := v.ValidateLicense(lic); err == nil {
			t.Fatal("ValidateLicense succeeded, want error")
		}
	})

	t.Run("delegation presented as root licence refused", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		pub, _ := generatePlatformKey(t)
		dlg := signDelegation(t, root, defaultDelegationClaims(now, pub))
		if _, err := v.ValidateLicense(dlg); err == nil {
			t.Fatal("ValidateLicense accepted a delegation as a licence")
		}
	})

	t.Run("HS256 licence refused", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, defaultLicenceClaims(now, "client1"))
		lic, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if _, err := v.ValidateLicense(lic); err == nil {
			t.Fatal("ValidateLicense accepted an HS256 token")
		}
	})

	t.Run("root RS256 licence still accepted", func(t *testing.T) {
		root := generateRootKey(t)
		v := newTestValidator(t, root)
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, defaultLicenceClaims(now, "client1"))
		lic, err := token.SignedString(root)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		got, err := v.ValidateLicense(lic)
		if err != nil {
			t.Fatalf("ValidateLicense: %v", err)
		}
		if got.Tier != TierEnterprise || got.Subject != "client1" {
			t.Errorf("got Tier=%q Subject=%q", got.Tier, got.Subject)
		}
		if got.Platform != "" || !got.DelegationExpiresAt.IsZero() {
			t.Errorf("Platform = %q, want empty for root licence", got.Platform)
		}
	})
}
