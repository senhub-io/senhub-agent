package license

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"

	"github.com/golang-jwt/jwt/v5"
)

// newTestValidator returns a validator trusting the given private key's
// public half, with one extra root key registered under kid when
// extraPEM is non-empty.
func newHardeningValidator(t *testing.T, pem string, extraKid, extraPEM string) *JWTValidator {
	t.Helper()
	v, err := NewJWTValidator(pem, 7)
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}
	if extraKid != "" {
		if err := v.AddRootKey(extraKid, extraPEM); err != nil {
			t.Fatalf("AddRootKey: %v", err)
		}
	}
	return v
}

// createSignedJWTWithKID is createSignedJWT with a kid header.
func createSignedJWTWithKID(t *testing.T, privateKey *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign JWT: %v", err)
	}
	return signed
}

// baseClaims is a valid licence payload the hardening tests mutate.
func baseClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []interface{}{"redfish"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
	}
}

func TestValidateLicense_RootKeySelection(t *testing.T) {
	rootPriv, rootPub := generateTestRSAKeys(t)
	otherPriv, otherPub := generateTestRSAKeys(t)
	rootPEM := publicKeyToPEM(rootPub)
	otherPEM := publicKeyToPEM(otherPub)
	v := newHardeningValidator(t, rootPEM, "2026-01", otherPEM)

	tests := []struct {
		name    string
		token   func() string
		wantErr error
	}{
		{
			name: "no kid uses historical key",
			token: func() string {
				return createSignedJWT(t, rootPriv, baseClaims())
			},
		},
		{
			name: "known kid uses rotated key",
			token: func() string {
				return createSignedJWTWithKID(t, otherPriv, "2026-01", baseClaims())
			},
		},
		{
			name: "unknown kid is refused",
			token: func() string {
				return createSignedJWTWithKID(t, otherPriv, "2099-99", baseClaims())
			},
			wantErr: ErrUnknownKeyID,
		},
		{
			name: "known kid signed by historical key is refused",
			token: func() string {
				return createSignedJWTWithKID(t, rootPriv, "2026-01", baseClaims())
			},
			wantErr: errAny,
		},
		{
			name: "HS256 is refused",
			token: func() string {
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, baseClaims())
				token.Header["kid"] = "2026-01"
				signed, err := token.SignedString([]byte("hmac-secret"))
				if err != nil {
					t.Fatalf("sign: %v", err)
				}
				return signed
			},
			wantErr: errAny,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateLicense(tt.token())
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateLicense() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateLicense() error = nil, want error")
			}
			if tt.wantErr != errAny && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateLicense() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// errAny stands for "any error" in the table above.
var errAny = errors.New("any error")

func TestAddRootKey(t *testing.T) {
	_, pub := generateTestRSAKeys(t)
	pemStr := publicKeyToPEM(pub)
	v, err := NewJWTValidator(pemStr, 7)
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}
	if err := v.AddRootKey("", pemStr); err == nil {
		t.Error("AddRootKey(empty kid) = nil, want error")
	}
	if err := v.AddRootKey("k1", "not a pem"); err == nil {
		t.Error("AddRootKey(bad PEM) = nil, want error")
	}
	if err := v.AddRootKey("k1", pemStr); err != nil {
		t.Fatalf("AddRootKey valid: %v", err)
	}
	if err := v.AddRootKey("k1", pemStr); err == nil {
		t.Error("AddRootKey(duplicate kid) = nil, want error")
	}
}

func TestValidateLicense_Tier(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	v := newHardeningValidator(t, publicKeyToPEM(pub), "", "")

	tests := []struct {
		name     string
		tier     interface{}
		wantTier LicenseTier
		wantErr  bool
	}{
		{name: "Pro normalised", tier: "Pro", wantTier: TierPro},
		{name: "pro", tier: "pro", wantTier: TierPro},
		{name: "ENTERPRISE normalised", tier: "ENTERPRISE", wantTier: TierEnterprise},
		{name: "missing tier rejected", tier: nil, wantErr: true},
		{name: "unknown tier rejected", tier: "gold", wantErr: true},
		{name: "empty tier rejected", tier: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := baseClaims()
			if tt.tier != nil {
				claims["tier"] = tt.tier
			} else {
				delete(claims, "tier")
			}
			lic, err := v.ValidateLicense(createSignedJWT(t, priv, claims))
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidTier) {
					t.Fatalf("ValidateLicense() error = %v, want ErrInvalidTier", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateLicense() error = %v", err)
			}
			if lic.Tier != tt.wantTier {
				t.Errorf("Tier = %q, want %q", lic.Tier, tt.wantTier)
			}
		})
	}
}

func TestValidateLicense_TransitionClaims(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	v := newHardeningValidator(t, publicKeyToPEM(pub), "", "")

	tests := []struct {
		name    string
		mutate  func(jwt.MapClaims)
		wantErr bool
	}{
		{name: "iss licence issuer accepted", mutate: func(c jwt.MapClaims) { c["iss"] = LicenceIssuer }},
		{name: "iss absent accepted", mutate: nil},
		{name: "iss wrong rejected", mutate: func(c jwt.MapClaims) { c["iss"] = "EvilCorp" }, wantErr: true},
		{name: "aud string wrong rejected", mutate: func(c jwt.MapClaims) { c["aud"] = "other-agent" }, wantErr: true},
		{name: "aud list wrong rejected", mutate: func(c jwt.MapClaims) { c["aud"] = []interface{}{"a", "b"} }, wantErr: true},
		{name: "aud list containing licence audience accepted", mutate: func(c jwt.MapClaims) { c["aud"] = []interface{}{"other", LicenceAudience} }},
		{name: "aud absent accepted", mutate: nil},
		{name: "nbf in one hour rejected", mutate: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }, wantErr: true},
		{name: "nbf in two minutes accepted", mutate: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(2 * time.Minute).Unix() }},
		{name: "nbf in the past accepted", mutate: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(-time.Hour).Unix() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := baseClaims()
			if tt.mutate != nil {
				tt.mutate(claims)
			}
			_, err := v.ValidateLicense(createSignedJWT(t, priv, claims))
			if tt.wantErr && err == nil {
				t.Fatal("ValidateLicense() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateLicense() error = %v", err)
			}
		})
	}
}

func TestVerifyBinding_InstanceID(t *testing.T) {
	agentKey := "0f14d0ab-9605-4a62-a9e4-5ed26688389b"
	instanceID := configuration.AgentInstanceID(agentKey)
	otherKey := "11111111-2222-3333-4444-555555555555"

	tests := []struct {
		name     string
		subject  string
		agentKey string
		want     bool
	}{
		{name: "agent key on this agent", subject: agentKey, agentKey: agentKey, want: true},
		{name: "instance id on this agent", subject: instanceID, agentKey: agentKey, want: true},
		{name: "instance id on another agent", subject: instanceID, agentKey: otherKey, want: false},
		{name: "empty subject accepted", subject: "", agentKey: otherKey, want: true},
		{name: "customer subject unchanged", subject: "client1", agentKey: otherKey, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lic := &License{Subject: tt.subject}
			if got := VerifyBinding("", tt.agentKey, lic); got != tt.want {
				t.Errorf("VerifyBinding() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A kid that is present but not a string names no key: reading it as
// absent would check the token against the historical key.
func TestNonStringKIDIsRefused(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	v, err := NewJWTValidator(pemKey, 7)
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"tier": "pro", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	token.Header["kid"] = 42
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.ValidateLicense(signed); !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("err = %v, want ErrUnknownKeyID", err)
	}
}
