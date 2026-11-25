package app

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/cliexit"
)

func newTestValidator(t *testing.T) (*license.JWTValidator, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshalling public key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	v, err := license.NewJWTValidator(string(pemBytes), 7)
	if err != nil {
		t.Fatalf("building validator: %v", err)
	}
	return v, key
}

func signRevocationList(t *testing.T, key *rsa.PrivateKey, ver int, revoked []string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"typ":     license.RevocationsClaimType,
		"ver":     ver,
		"iat":     time.Now().Unix(),
		"revoked": revoked,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["typ"] = license.RevocationsHeaderType
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("signing list: %v", err)
	}
	return s
}

func signLicenseToken(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(24 * time.Hour).Unix(),
		"tier": "pro",
		"jti":  "jti-1",
		"sub":  "",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("signing licence: %v", err)
	}
	return s
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(configPath, []byte("agent:\n  key: test-key\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return configPath
}

func TestRunLicenseRevocationsImport(t *testing.T) {
	v, key := newTestValidator(t)
	configPath := writeTestConfig(t)
	sidecar := configuration.RevocationsSidecarPath(configPath)

	v1 := signRevocationList(t, key, 1, []string{"jti-1"})
	v1bis := signRevocationList(t, key, 1, []string{"jti-1"})
	v2 := signRevocationList(t, key, 2, []string{"jti-1", "jti-2"})
	licTok := signLicenseToken(t, key)

	// A licence token is not a revocation list: refuse and write nothing.
	var out, errOut bytes.Buffer
	if code := runLicenseRevocationsImport(v, configPath, licTok, &out, &errOut); code != cliexit.Failure {
		t.Fatalf("licence token: code = %d, want %d", code, cliexit.Failure)
	}
	if !strings.Contains(errOut.String(), "Error") {
		t.Fatalf("licence token: no error on stderr: %q", errOut.String())
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("licence token: sidecar must stay absent, stat err = %v", err)
	}

	// Fresh import of v1.
	out.Reset()
	errOut.Reset()
	if code := runLicenseRevocationsImport(v, configPath, v1, &out, &errOut); code != cliexit.OK {
		t.Fatalf("import v1: code = %d, want %d (stderr %q)", code, cliexit.OK, errOut.String())
	}
	if !strings.Contains(out.String(), "version 1 imported") {
		t.Fatalf("import v1: output = %q", out.String())
	}
	written, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("import v1: reading sidecar: %v", err)
	}
	if strings.TrimSpace(string(written)) != v1 {
		t.Fatalf("import v1: sidecar content mismatch")
	}

	// Re-importing the very same token is a no-op success.
	out.Reset()
	errOut.Reset()
	if code := runLicenseRevocationsImport(v, configPath, v1, &out, &errOut); code != cliexit.OK {
		t.Fatalf("reimport v1: code = %d, want %d", code, cliexit.OK)
	}
	if !strings.Contains(out.String(), "already in place") {
		t.Fatalf("reimport v1: output = %q", out.String())
	}

	// Import v2, then v1 must be refused as not newer.
	out.Reset()
	errOut.Reset()
	if code := runLicenseRevocationsImport(v, configPath, v2, &out, &errOut); code != cliexit.OK {
		t.Fatalf("import v2: code = %d, want %d", code, cliexit.OK)
	}
	out.Reset()
	errOut.Reset()
	if code := runLicenseRevocationsImport(v, configPath, v1bis, &out, &errOut); code != cliexit.Failure {
		t.Fatalf("v1 after v2: code = %d, want %d", code, cliexit.Failure)
	}
	if !strings.Contains(errOut.String(), "not newer") {
		t.Fatalf("v1 after v2: stderr = %q", errOut.String())
	}
	written, err = os.ReadFile(sidecar)
	if err != nil || strings.TrimSpace(string(written)) != v2 {
		t.Fatalf("v1 after v2: sidecar must still hold v2 (err %v)", err)
	}
}

func TestRefuseWhenLicenceFromServer(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"flag true", "opamp:\n  license_from_server: true\n", true},
		{"no block", "agent:\n  key: k\n", false},
		{"flag false", "opamp:\n  license_from_server: false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "agent.yaml")
			if err := os.WriteFile(configPath, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("writing config: %v", err)
			}
			var errOut bytes.Buffer
			if got := refuseWhenLicenceFromServer(configPath, &errOut); got != tc.want {
				t.Fatalf("refuse = %v, want %v (stderr %q)", got, tc.want, errOut.String())
			}
			if tc.want && !strings.Contains(errOut.String(), "second writer") {
				t.Fatalf("missing refusal message: %q", errOut.String())
			}
		})
	}
}

// An agent.yaml that cannot be read cannot tell who owns the licence: the
// local write is refused rather than risking a second writer.
func TestRefuseWhenLicenceFromServer_UnreadableConfigRefuses(t *testing.T) {
	var errOut bytes.Buffer
	if !refuseWhenLicenceFromServer(filepath.Join(t.TempDir(), "missing.yaml"), &errOut) {
		t.Fatal("an unreadable agent.yaml let a local licence write through")
	}
}
