package sensor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func testLicenseSetup(t *testing.T) (*rsa.PrivateKey, *license.JWTValidator, *MockConfigProvider, *sensor) {
	t.Helper()
	t.Cleanup(func() {
		agentstate.ResetLicenseInvalidForTest()
		license.ResetCurrentForTest()
	})

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

	provider := &MockConfigProvider{config: configuration.ConfigurationData{Probes: []configuration.ProbeConfig{}}}
	addDataPoint := func(data []datapoint.DataPoint, router data_store.StrategyRouter) error { return nil }
	s := &sensor{
		addDataPoint:     addDataPoint,
		configProvider:   provider,
		moduleLogger:     logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "sensor-test"),
		licenseValidator: validator,
		resolver:         license.NewResolver(validator),
	}
	return key, validator, provider, s
}

func signTestToken(t *testing.T, key *rsa.PrivateKey, headerTyp string, claims jwt.MapClaims) string {
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

func testLicenseClaims(tier, jti, sub string) jwt.MapClaims {
	return jwt.MapClaims{
		"tier":     tier,
		"sub":      sub,
		"jti":      jti,
		"platform": "senhub.example",
		"iat":      time.Now().Unix(),
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}
}

func testRevocationClaims(ver uint64, revoked []string) jwt.MapClaims {
	return jwt.MapClaims{
		"typ":     license.RevocationsClaimType,
		"ver":     ver,
		"iat":     time.Now().Unix(),
		"revoked": revoked,
	}
}

func TestSensor_RefreshLicense_NewTokenWithoutRestart(t *testing.T) {
	key, _, provider, s := testLicenseSetup(t)
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	provider.config.Agent.AuthenticationKey = agentKey

	provider.config.Agent.License = signTestToken(t, key, "", testLicenseClaims("pro", "j1", agentKey))
	s.refreshLicenseLocked(provider.GetConfiguration())
	if s.license == nil || s.license.Tier != license.TierPro {
		t.Fatalf("first sync: tier=%v, want pro", s.license)
	}

	provider.config.Agent.License = signTestToken(t, key, "", testLicenseClaims("enterprise", "j2", agentKey))
	if !s.refreshLicenseLocked(provider.GetConfiguration()) {
		t.Fatal("second sync reported no change")
	}
	if s.license == nil || s.license.Tier != license.TierEnterprise {
		t.Fatalf("license tier after replacement = %v, want enterprise without restart", s.license)
	}
}

func TestSensor_RefreshLicense_RevokedByList(t *testing.T) {
	key, _, provider, s := testLicenseSetup(t)
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	provider.config.Agent.AuthenticationKey = agentKey
	provider.config.Agent.License = signTestToken(t, key, "", testLicenseClaims("pro", "j1", agentKey))
	s.refreshLicenseLocked(provider.GetConfiguration())

	provider.config.Agent.LicenseRevocations = signTestToken(t, key, license.RevocationsHeaderType, testRevocationClaims(1, []string{"j1"}))
	s.refreshLicenseLocked(provider.GetConfiguration())

	if s.license != nil {
		t.Errorf("license still set after revocation (tier=%v), want nil", s.license.Tier)
	}
	f, ok := license.Current()
	if !ok || f.State != license.StateRevoked {
		t.Errorf("license.Current() = %v ok=%v, want StateRevoked", f.State, ok)
	}
	if agentstate.GetLicenseInvalidByReason()["revoked"] != 1 {
		t.Errorf("license.invalid{reason=revoked} = %v, want 1", agentstate.GetLicenseInvalidByReason()["revoked"])
	}
}

func TestSensor_RefreshLicense_BadListIgnored(t *testing.T) {
	key, _, provider, s := testLicenseSetup(t)
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	provider.config.Agent.AuthenticationKey = agentKey
	provider.config.Agent.License = signTestToken(t, key, "", testLicenseClaims("pro", "j1", agentKey))
	s.refreshLicenseLocked(provider.GetConfiguration())

	// A licence token is not a revocation list: it must be rejected without
	// affecting the licence in force.
	provider.config.Agent.LicenseRevocations = provider.config.Agent.License
	s.refreshLicenseLocked(provider.GetConfiguration())

	if s.license == nil || s.license.Tier != license.TierPro {
		t.Fatalf("license lost after a mistyped revocation list, tier=%v", s.license)
	}
	if f, _ := license.Current(); f.Revocations != nil {
		t.Errorf("revocation list of wrong type was applied: %+v", f.Revocations)
	}
}

func TestSensor_RefreshLicense_NoChangeReturnsFalse(t *testing.T) {
	key, _, provider, s := testLicenseSetup(t)
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	provider.config.Agent.AuthenticationKey = agentKey
	provider.config.Agent.License = signTestToken(t, key, "", testLicenseClaims("pro", "j1", agentKey))

	if !s.refreshLicenseLocked(provider.GetConfiguration()) {
		t.Fatal("first refresh reported no change")
	}
	if s.refreshLicenseLocked(provider.GetConfiguration()) {
		t.Fatal("identical second refresh reported a change")
	}
}

// authorizingFake honors the licence passed to IsProbeAuthorized, unlike
// fakeLicenseValidator which always authorizes.
type authorizingFake struct {
	fakeLicenseValidator
}

func (a *authorizingFake) IsProbeAuthorized(lic *license.License, probeType string) bool {
	return lic != nil
}

func TestSensor_CheckLicenceAllows(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	lic := &license.License{Tier: license.TierEnterprise, AuthorizedProbes: []string{"*"}}
	validator := &authorizingFake{fakeLicenseValidator{lic: lic}}
	s := &sensor{
		moduleLogger:     logger.NewModuleLogger(baseLogger, "sensor-test"),
		licenseValidator: validator,
	}

	paid := license.KnownPaidProbes()[0]
	cfg := configuration.ProbeConfig{Name: "p", Type: paid}
	if s.checkLicenceAllows(cfg) == nil {
		t.Errorf("paid probe %q allowed without a license, want refused", paid)
	}
	s.license = lic
	if err := s.checkLicenceAllows(cfg); err != nil {
		t.Errorf("paid probe %q refused with a wildcard license: %v", paid, err)
	}
}

// powerstore is a paid type the OSS build does not carry; the test binary
// registers it as a cpu probe so a paid probe can run here.
func init() {
	probes.RegisterProbe("powerstore", func(params map[string]interface{}, l *logger.Logger) (types.Probe, error) {
		cpu, _ := probes.LookupProbeConstructor("cpu")
		return cpu(params, l)
	})
}

// A paid probe already running stops when the licence that allowed it is
// revoked: authorisation used to be checked only when a probe started.
func TestSensor_RevokedLicenceStopsARunningPaidProbe(t *testing.T) {
	key, validator, provider, _ := testLicenseSetup(t)
	agentKey := "aaaaaaaa-1111-4111-8111-111111111111"
	provider.config.Agent.AuthenticationKey = agentKey
	provider.config.Agent.License = signTestToken(t, key, "", jwt.MapClaims{
		"tier": "pro", "sub": agentKey, "jti": "j1", "authorized_probes": []string{"powerstore"},
		"iat": time.Now().Unix(), "exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	provider.config.Probes = []configuration.ProbeConfig{{Name: "ps", Type: "powerstore", Params: map[string]interface{}{"interval": 30}}}

	add := func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil }
	s := NewSensor(add, provider, logger.NewLogger(&cliArgs.ParsedArgs{})).(*sensor)
	s.licenseValidator = validator
	s.resolver = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()
	if n := len(s.startedProbes); n != 1 {
		t.Fatalf("running probes = %d before revocation, want 1", n)
	}

	provider.config.Agent.LicenseRevocations = signTestToken(t, key, license.RevocationsHeaderType, testRevocationClaims(1, []string{"j1"}))
	if err := s.SyncConfiguration(); err != nil {
		t.Fatal(err)
	}
	if n := len(s.startedProbes); n != 0 {
		t.Fatalf("running probes = %d after revocation, want 0", n)
	}
}
