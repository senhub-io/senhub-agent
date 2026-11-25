package extensionhost

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

type fakeConfigSource struct {
	mu      sync.Mutex
	cfg     configuration.ConfigurationData
	path    string
	reloads int
}

func (f *fakeConfigSource) ExtensionBlock(string) (map[string]interface{}, bool) { return nil, false }
func (f *fakeConfigSource) OnConfigChanged(func(string))                         {}
func (f *fakeConfigSource) GetAuthenticationKey() string {
	return f.cfg.Agent.AuthenticationKey
}
func (f *fakeConfigSource) GetConfigPath() string { return f.path }
func (f *fakeConfigSource) GetConfiguration() configuration.ConfigurationData {
	return f.cfg
}
func (f *fakeConfigSource) Reload() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	return nil
}
func (f *fakeConfigSource) reloadsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloads
}

func newApplyTestHost(t *testing.T) (*host, *fakeConfigSource, *rsa.PrivateKey) {
	t.Helper()
	t.Cleanup(func() {
		newLicenseValidator = func() (license.Validator, error) { return license.GetDefaultValidator(7) }
	})
	license.ResetCurrentForTest()
	t.Cleanup(license.ResetCurrentForTest)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	validator, err := license.NewJWTValidator(pemStr, 7)
	if err != nil {
		t.Fatalf("NewJWTValidator: %v", err)
	}
	newLicenseValidator = func() (license.Validator, error) { return validator, nil }

	dir := t.TempDir()
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("agent:\n  key: "+agentKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &fakeConfigSource{
		cfg:  configuration.ConfigurationData{Agent: configuration.AgentConfig{AuthenticationKey: agentKey}},
		path: filepath.Join(dir, "agent.yaml"),
	}
	h := &host{
		deps: Deps{
			Logger:            logger.NewLogger(&cliArgs.ParsedArgs{}),
			Config:            src,
			IsProbeRegistered: func(string) bool { return true },
		},
		log: logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "extension-test"),
	}
	return h, src, key
}

func signApplyToken(t *testing.T, key *rsa.PrivateKey, headerTyp string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if headerTyp != "" {
		tok.Header["typ"] = headerTyp
	}
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

func applyLicenseToken(t *testing.T, key *rsa.PrivateKey, tier, jti, sub string) string {
	return signApplyToken(t, key, "", jwt.MapClaims{
		"tier":     tier,
		"sub":      sub,
		"jti":      jti,
		"platform": "senhub.example",
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})
}

func applyRevocationToken(t *testing.T, key *rsa.PrivateKey, ver uint64, revoked []string) string {
	return signApplyToken(t, key, license.RevocationsHeaderType, jwt.MapClaims{
		"typ":     license.RevocationsClaimType,
		"ver":     ver,
		"iat":     time.Now().Unix(),
		"revoked": revoked,
	})
}

func refusalReason(t *testing.T, err error) string {
	t.Helper()
	var ref *extension.LicenseRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("err = %v, want *extension.LicenseRefusal", err)
	}
	return ref.Reason
}

func TestApplyLicense_BindingRefused(t *testing.T) {
	h, src, key := newApplyTestHost(t)
	token := applyLicenseToken(t, key, "pro", "j1", "bbbbbbbb-2222-4222-8222-222222222222")

	if err := h.ApplyLicense(token); err == nil {
		t.Fatal("licence bound to another agent was accepted")
	} else if reason := refusalReason(t, err); reason != "binding" {
		t.Errorf("refusal reason = %q, want binding", reason)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(src.path), "license.jwt")); !os.IsNotExist(err) {
		t.Errorf("license.jwt written for a refused licence (err=%v)", err)
	}
	if src.reloadsCount() != 0 {
		t.Errorf("Reload called %d times for a refused licence, want 0", src.reloadsCount())
	}
}

func TestApplyLicense_ValidWrittenAndReloaded(t *testing.T) {
	h, src, key := newApplyTestHost(t)
	token := applyLicenseToken(t, key, "pro", "j1", src.cfg.Agent.AuthenticationKey)

	if err := h.ApplyLicense(token); err != nil {
		t.Fatalf("ApplyLicense: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(src.path), "license.jwt")); err != nil {
		t.Errorf("license.jwt not written: %v", err)
	}
	if n := src.reloadsCount(); n != 1 {
		t.Errorf("Reload called %d times, want 1", n)
	}
}

func TestApplyRevocations_ReplayIsNoop(t *testing.T) {
	h, src, key := newApplyTestHost(t)
	token := applyRevocationToken(t, key, 2, []string{"j1"})

	if err := h.ApplyRevocations(token); err != nil {
		t.Fatalf("first ApplyRevocations: %v", err)
	}
	if err := h.ApplyRevocations(token); err != nil {
		t.Fatalf("replayed ApplyRevocations: %v", err)
	}
	if n := src.reloadsCount(); n != 1 {
		t.Errorf("Reload called %d times for a replayed list, want 1", n)
	}
}

func TestApplyRevocations_StaleRefused(t *testing.T) {
	h, _, key := newApplyTestHost(t)
	if err := h.ApplyRevocations(applyRevocationToken(t, key, 2, []string{"j1"})); err != nil {
		t.Fatalf("first ApplyRevocations: %v", err)
	}
	err := h.ApplyRevocations(applyRevocationToken(t, key, 1, []string{"j1"}))
	if err == nil {
		t.Fatal("older list accepted")
	}
	if reason := refusalReason(t, err); reason != "stale" {
		t.Errorf("refusal reason = %q, want stale", reason)
	}
}

func TestApplyRevocations_WrongType(t *testing.T) {
	h, _, key := newApplyTestHost(t)
	lic := applyLicenseToken(t, key, "pro", "j1", h.deps.Config.GetAuthenticationKey())
	err := h.ApplyRevocations(lic)
	if err == nil {
		t.Fatal("licence token accepted as a revocation list")
	}
	if reason := refusalReason(t, err); reason != "wrong_type" {
		t.Errorf("refusal reason = %q, want wrong_type", reason)
	}
}

// Fleet resends the licence on every reconnection: the one in force is
// neither rewritten nor reloaded.
func TestApplyLicense_LicenceInForceIsNotRewritten(t *testing.T) {
	h, src, key := newApplyTestHost(t)
	token := applyLicenseToken(t, key, "pro", "j1", src.cfg.Agent.AuthenticationKey)
	src.cfg.Agent.License = token

	if err := h.ApplyLicense(token); err != nil {
		t.Fatalf("ApplyLicense: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(src.path), "license.jwt")); !os.IsNotExist(err) {
		t.Errorf("license.jwt rewritten for the licence in force (err=%v)", err)
	}
	if n := src.reloadsCount(); n != 0 {
		t.Errorf("Reload called %d times, want 0", n)
	}
}
