package license

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestFreeTierProbes(t *testing.T) {
	tests := []struct {
		name      string
		probeName string
		expected  bool
	}{
		{"CPU probe is free tier", "cpu", true},
		{"Memory probe is free tier", "memory", true},
		{"LogicalDisk probe is free tier", "logicaldisk", true},
		{"Network probe is free tier", "network", true},
		{"LinuxLogs probe is free tier", "linux_logs", true},
		{"WindowsEventLog probe is free tier", "windows_eventlog", true},
		{"FileTail probe is free tier", "filetail", true},
		{"OTLPReceiver probe is free tier", "otlp_receiver", true},
		{"SNMPTrap probe is free tier", "snmp_trap", true},
		{"Redfish probe is NOT free tier", "redfish", false},
		{"Citrix probe is NOT free tier", "citrix", false},
		{"WebApp probe is NOT free tier", "ping_webapp", false},
		{"Gateway probe is NOT free tier", "ping_gateway", false},
		{"Syslog probe IS free tier (#298)", "syslog", true},
		{"CouchDB probe is free tier", "couchdb", true},
		{"Event probe is NOT free tier", "event", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFreeTierProbe(tt.probeName)
			if result != tt.expected {
				t.Errorf("isFreeTierProbe(%q) = %v, want %v", tt.probeName, result, tt.expected)
			}
		})
	}
}

func TestGetFreeTierProbes(t *testing.T) {
	probes := GetFreeTierProbes()

	// Check we have exactly 18 free tier probes
	if len(probes) != 18 {
		t.Errorf("GetFreeTierProbes() returned %d probes, want 18", len(probes))
	}

	// Check all expected probes are present
	expectedProbes := map[string]bool{
		"cpu":               false,
		"memory":            false,
		"logicaldisk":       false,
		"network":           false,
		"linux_logs":        false,
		"windows_eventlog":  false,
		"filetail":          false,
		"snmp_poll":         false,
		"otlp_receiver":     false,
		"snmp_trap":         false,
		"icmp_check":        false,
		"http_check":        false,
		"dns_latency":       false,
		"tcp_dial":          false,
		"prometheus_scrape": false,
		"exec":              false,
		"syslog":            false,
		"couchdb":           false,
	}

	for _, probe := range probes {
		if _, exists := expectedProbes[probe]; !exists {
			t.Errorf("Unexpected probe %q in free tier list", probe)
		}
		expectedProbes[probe] = true
	}

	// Check all were found
	for probe, found := range expectedProbes {
		if !found {
			t.Errorf("Expected probe %q not found in free tier list", probe)
		}
	}
}

func TestMockValidator_ValidateLicense(t *testing.T) {
	validator := NewMockValidator(7)

	// Use dynamic dates to avoid test failures when hardcoded dates expire
	futureDate := time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)
	pastIssuedDate := time.Now().Add(-180 * 24 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name        string
		token       string
		expectError bool
		checkResult func(*testing.T, *License)
	}{
		{
			name: "Valid Pro license",
			token: `{
				"tier": "pro",
				"authorized_probes": ["*"],
				"expires_at": "` + futureDate + `",
				"issued_at": "` + pastIssuedDate + `",
				"subject": "customer-123"
			}`,
			expectError: false,
			checkResult: func(t *testing.T, lic *License) {
				if lic.Tier != TierPro {
					t.Errorf("Expected tier 'pro', got %q", lic.Tier)
				}
				if len(lic.AuthorizedProbes) != 1 || lic.AuthorizedProbes[0] != "*" {
					t.Errorf("Expected authorized_probes ['*'], got %v", lic.AuthorizedProbes)
				}
				if lic.IsExpired {
					t.Errorf("License should not be expired")
				}
			},
		},
		{
			name: "Expired license",
			token: `{
				"tier": "pro",
				"authorized_probes": ["redfish", "citrix"],
				"expires_at": "2020-01-01T00:00:00Z",
				"issued_at": "2019-01-01T00:00:00Z",
				"subject": "customer-456"
			}`,
			expectError: false,
			checkResult: func(t *testing.T, lic *License) {
				if !lic.IsExpired {
					t.Errorf("License should be expired")
				}
				if len(lic.AuthorizedProbes) != 2 {
					t.Errorf("Expected 2 authorized probes, got %d", len(lic.AuthorizedProbes))
				}
			},
		},
		{
			name:        "Invalid JSON",
			token:       `{invalid json`,
			expectError: true,
		},
		{
			name: "Invalid date format",
			token: `{
				"tier": "pro",
				"authorized_probes": ["*"],
				"expires_at": "invalid-date",
				"issued_at": "2025-01-01T00:00:00Z",
				"subject": "customer-789"
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			license, err := validator.ValidateLicense(tt.token)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.checkResult != nil {
				tt.checkResult(t, license)
			}
		})
	}
}

func TestMockValidator_IsProbeAuthorized(t *testing.T) {
	validator := NewMockValidator(7)

	// Create valid license with specific probes
	validLicense := &License{
		Tier:             TierPro,
		AuthorizedProbes: []string{"redfish", "citrix"},
		ExpiresAt:        time.Now().Add(365 * 24 * time.Hour),
		IssuedAt:         time.Now().Add(-30 * 24 * time.Hour),
		Subject:          "test-customer",
		IsExpired:        false,
		GracePeriodDays:  7,
	}

	// Create expired license (outside grace period)
	expiredLicense := &License{
		Tier:             TierPro,
		AuthorizedProbes: []string{"*"},
		ExpiresAt:        time.Now().Add(-30 * 24 * time.Hour), // Expired 30 days ago
		IssuedAt:         time.Now().Add(-365 * 24 * time.Hour),
		Subject:          "test-customer",
		IsExpired:        true,
		GracePeriodDays:  7,
	}

	// Create license in grace period
	gracePeriodLicense := &License{
		Tier:             TierPro,
		AuthorizedProbes: []string{"*"},
		ExpiresAt:        time.Now().Add(-3 * 24 * time.Hour), // Expired 3 days ago
		IssuedAt:         time.Now().Add(-365 * 24 * time.Hour),
		Subject:          "test-customer",
		IsExpired:        true,
		GracePeriodDays:  7,
	}

	// Create wildcard license
	wildcardLicense := &License{
		Tier:             TierEnterprise,
		AuthorizedProbes: []string{"*"},
		ExpiresAt:        time.Now().Add(365 * 24 * time.Hour),
		IssuedAt:         time.Now().Add(-30 * 24 * time.Hour),
		Subject:          "test-customer",
		IsExpired:        false,
		GracePeriodDays:  7,
	}

	tests := []struct {
		name       string
		license    *License
		probeName  string
		authorized bool
	}{
		// Free tier probes - always authorized
		{"Free tier: CPU without license", nil, "cpu", true},
		{"Free tier: Memory without license", nil, "memory", true},
		{"Free tier: LogicalDisk without license", nil, "logicaldisk", true},
		{"Free tier: Network without license", nil, "network", true},
		{"Free tier: CPU with license", validLicense, "cpu", true},

		// ONLINE MODE BYPASS: Paid probes without license - AUTHORIZED (Enterprise behavior)
		// This allows online agents to work without explicit licenses
		{"Paid: Redfish without license (online mode)", nil, "redfish", true},
		{"Paid: Citrix without license (online mode)", nil, "citrix", true},
		{"Paid: WebApp without license (online mode)", nil, "ping_webapp", true},

		// Paid probes with valid license
		{"Valid license: Authorized probe (redfish)", validLicense, "redfish", true},
		{"Valid license: Authorized probe (citrix)", validLicense, "citrix", true},
		{"Valid license: Unauthorized probe", validLicense, "ping_webapp", false},

		// Wildcard license
		{"Wildcard license: Any probe", wildcardLicense, "redfish", true},
		{"Wildcard license: Another probe", wildcardLicense, "citrix", true},
		{"Wildcard license: Third probe", wildcardLicense, "ping_webapp", true},

		// Expired license (outside grace period)
		{"Expired license: Free tier probe", expiredLicense, "cpu", true},
		{"Expired license: Paid probe", expiredLicense, "redfish", false},

		// Grace period license
		{"Grace period: Free tier probe", gracePeriodLicense, "cpu", true},
		{"Grace period: Paid probe", gracePeriodLicense, "redfish", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsProbeAuthorized(tt.license, tt.probeName)
			if result != tt.authorized {
				t.Errorf("IsProbeAuthorized(%v, %q) = %v, want %v",
					tt.license, tt.probeName, result, tt.authorized)
			}
		})
	}
}

func TestMockValidator_IsInGracePeriod(t *testing.T) {
	validator := NewMockValidator(7)

	tests := []struct {
		name          string
		license       *License
		inGracePeriod bool
	}{
		{
			name: "Not expired",
			license: &License{
				ExpiresAt:       time.Now().Add(30 * 24 * time.Hour),
				IsExpired:       false,
				GracePeriodDays: 7,
			},
			inGracePeriod: false,
		},
		{
			name: "Expired within grace period (3 days ago)",
			license: &License{
				ExpiresAt:       time.Now().Add(-3 * 24 * time.Hour),
				IsExpired:       true,
				GracePeriodDays: 7,
			},
			inGracePeriod: true,
		},
		{
			name: "Expired outside grace period (10 days ago)",
			license: &License{
				ExpiresAt:       time.Now().Add(-10 * 24 * time.Hour),
				IsExpired:       true,
				GracePeriodDays: 7,
			},
			inGracePeriod: false,
		},
		{
			name: "Expired exactly at grace period boundary",
			license: &License{
				ExpiresAt:       time.Now().Add(-7 * 24 * time.Hour),
				IsExpired:       true,
				GracePeriodDays: 7,
			},
			inGracePeriod: false, // Exactly at boundary = outside
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsInGracePeriod(tt.license)
			if result != tt.inGracePeriod {
				t.Errorf("IsInGracePeriod() = %v, want %v", result, tt.inGracePeriod)
			}
		})
	}
}

func TestLicenseTiers(t *testing.T) {
	// Test that tier constants are defined correctly
	if TierFree != "free" {
		t.Errorf("TierFree = %q, want 'free'", TierFree)
	}
	if TierPro != "pro" {
		t.Errorf("TierPro = %q, want 'pro'", TierPro)
	}
	if TierEnterprise != "enterprise" {
		t.Errorf("TierEnterprise = %q, want 'enterprise'", TierEnterprise)
	}
}

// Test helpers for JWT validation

func generateTestRSAKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048) // 2048 for faster tests
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}
	return privateKey, &privateKey.PublicKey
}

func createSignedJWT(t *testing.T, privateKey *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign JWT: %v", err)
	}
	return signedToken
}

func createInvalidSignatureJWT(t *testing.T) string {
	t.Helper()
	// Create token with different key than validator uses
	differentKey, _ := generateTestRSAKeys(t)
	claims := jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []string{"redfish"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               "test-customer",
	}
	return createSignedJWT(t, differentKey, claims)
}

func publicKeyToPEM(publicKey *rsa.PublicKey) string {
	publicKeyBytes, _ := x509.MarshalPKIXPublicKey(publicKey)
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})
	return string(publicKeyPEM)
}

// JWT Validator Tests

func TestJWTValidator_ValidateLicense_ValidToken(t *testing.T) {
	// Generate test RSA keys
	privateKey, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator with test public key
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create valid JWT token
	claims := jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []interface{}{"redfish", "citrix"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               "test-customer",
	}
	tokenString := createSignedJWT(t, privateKey, claims)

	// Validate token
	license, err := validator.ValidateLicense(tokenString)
	if err != nil {
		t.Fatalf("ValidateLicense() failed: %v", err)
	}

	// Verify license fields
	if license.Tier != TierPro {
		t.Errorf("License tier = %q, want %q", license.Tier, TierPro)
	}
	if len(license.AuthorizedProbes) != 2 {
		t.Errorf("AuthorizedProbes length = %d, want 2", len(license.AuthorizedProbes))
	}
	if license.Subject != "test-customer" {
		t.Errorf("Subject = %q, want %q", license.Subject, "test-customer")
	}
	if license.IsExpired {
		t.Error("License should not be expired")
	}
}

func TestJWTValidator_ValidateLicense_InvalidSignature(t *testing.T) {
	// Generate test RSA keys
	_, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator with test public key
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create token signed with DIFFERENT key
	tokenString := createInvalidSignatureJWT(t)

	// Validation should fail
	_, err = validator.ValidateLicense(tokenString)
	if err == nil {
		t.Error("ValidateLicense() should fail for invalid signature")
	}
}

func TestJWTValidator_ValidateLicense_WrongAlgorithm(t *testing.T) {
	// Generate test RSA keys
	_, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create token with HMAC algorithm (not RSA)
	claims := jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []interface{}{"redfish"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               "test-customer",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("secret"))

	// Validation should fail
	_, err = validator.ValidateLicense(tokenString)
	if err == nil {
		t.Error("ValidateLicense() should fail for HMAC algorithm")
	}
}

func TestJWTValidator_ValidateLicense_ExpiredToken(t *testing.T) {
	// Generate test RSA keys
	privateKey, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create expired JWT token
	claims := jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []interface{}{"redfish"},
		"exp":               time.Now().Add(-10 * 24 * time.Hour).Unix(), // Expired 10 days ago
		"iat":               time.Now().Add(-365 * 24 * time.Hour).Unix(),
		"iss":               "SenHub",
		"sub":               "test-customer",
	}
	tokenString := createSignedJWT(t, privateKey, claims)

	// Validate token (should succeed but IsExpired should be true)
	license, err := validator.ValidateLicense(tokenString)
	if err != nil {
		t.Fatalf("ValidateLicense() failed: %v", err)
	}

	if !license.IsExpired {
		t.Error("License should be marked as expired")
	}
}

func TestJWTValidator_ValidateLicense_MalformedJWT(t *testing.T) {
	// Generate test RSA keys
	_, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"Not a JWT", "not.a.jwt.token"},
		{"Empty string", ""},
		{"Only two parts", "header.payload"},
		{"Invalid base64", "invalid!!!.base64!!.data!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.ValidateLicense(tt.token)
			if err == nil {
				t.Errorf("ValidateLicense() should fail for malformed JWT: %q", tt.token)
			}
		})
	}
}

func TestJWTValidator_ValidateLicense_EnterpriseWildcard(t *testing.T) {
	// Generate test RSA keys
	privateKey, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create enterprise JWT with wildcard
	claims := jwt.MapClaims{
		"tier":              "enterprise",
		"authorized_probes": []interface{}{"*"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               "enterprise-customer",
	}
	tokenString := createSignedJWT(t, privateKey, claims)

	// Validate token
	license, err := validator.ValidateLicense(tokenString)
	if err != nil {
		t.Fatalf("ValidateLicense() failed: %v", err)
	}

	// Verify enterprise tier with wildcard
	if license.Tier != TierEnterprise {
		t.Errorf("License tier = %q, want %q", license.Tier, TierEnterprise)
	}
	if len(license.AuthorizedProbes) != 1 || license.AuthorizedProbes[0] != "*" {
		t.Errorf("AuthorizedProbes = %v, want ['*']", license.AuthorizedProbes)
	}

	// Verify any probe is authorized
	if !validator.IsProbeAuthorized(license, "any_probe_name") {
		t.Error("Enterprise license should authorize any probe")
	}
}

func TestJWTValidator_IsProbeAuthorized(t *testing.T) {
	// Generate test RSA keys
	privateKey, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	// Create Pro license with specific probes
	claims := jwt.MapClaims{
		"tier":              "pro",
		"authorized_probes": []interface{}{"redfish", "citrix"},
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(),
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               "test-customer",
	}
	tokenString := createSignedJWT(t, privateKey, claims)
	license, _ := validator.ValidateLicense(tokenString)

	tests := []struct {
		name       string
		probe      string
		authorized bool
	}{
		{"Authorized: redfish", "redfish", true},
		{"Authorized: citrix", "citrix", true},
		{"Free tier: cpu", "cpu", true},
		{"Free tier: memory", "memory", true},
		{"Free tier (#298): syslog", "syslog", true},
		{"Unauthorized: event", "event", false},
		{"Unauthorized: veeam", "veeam", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsProbeAuthorized(license, tt.probe)
			if result != tt.authorized {
				t.Errorf("IsProbeAuthorized(%q) = %v, want %v", tt.probe, result, tt.authorized)
			}
		})
	}
}

func TestJWTValidator_IsInGracePeriod(t *testing.T) {
	// Generate test RSA keys
	privateKey, publicKey := generateTestRSAKeys(t)
	publicKeyPEM := publicKeyToPEM(publicKey)

	// Create validator with 7-day grace period
	validator, err := NewJWTValidator(publicKeyPEM, 7)
	if err != nil {
		t.Fatalf("Failed to create JWT validator: %v", err)
	}

	tests := []struct {
		name           string
		expiredDaysAgo int
		inGracePeriod  bool
	}{
		{"Expired 3 days ago (in grace)", 3, true},
		{"Expired 5 days ago (in grace)", 5, true},
		{"Expired 6 days ago (in grace)", 6, true},
		{"Expired 7 days ago (outside grace)", 7, false}, // Exactly 7 days is outside (Before, not BeforeOrEqual)
		{"Expired 10 days ago (outside grace)", 10, false},
		{"Not expired", -5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create license with specified expiration
			claims := jwt.MapClaims{
				"tier":              "pro",
				"authorized_probes": []interface{}{"redfish"},
				"exp":               time.Now().Add(time.Duration(-tt.expiredDaysAgo) * 24 * time.Hour).Unix(),
				"iat":               time.Now().Add(-365 * 24 * time.Hour).Unix(),
				"iss":               "SenHub",
				"sub":               "test-customer",
			}
			tokenString := createSignedJWT(t, privateKey, claims)
			license, _ := validator.ValidateLicense(tokenString)

			result := validator.IsInGracePeriod(license)
			if result != tt.inGracePeriod {
				t.Errorf("IsInGracePeriod() = %v, want %v", result, tt.inGracePeriod)
			}
		})
	}
}

func TestNewJWTValidator_InvalidPublicKey(t *testing.T) {
	tests := []struct {
		name      string
		publicKey string
	}{
		{"Empty string", ""},
		{"Invalid PEM", "not a valid pem block"},
		{"Wrong key type", "-----BEGIN PRIVATE KEY-----\nMIIEv..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJWTValidator(tt.publicKey, 7)
			if err == nil {
				t.Error("NewJWTValidator() should fail for invalid public key")
			}
		})
	}
}

func TestGetDefaultValidator(t *testing.T) {
	// Test that GetDefaultValidator returns a valid validator with embedded public key
	validator, err := GetDefaultValidator(7)
	if err != nil {
		t.Fatalf("GetDefaultValidator() failed: %v", err)
	}

	if validator == nil {
		t.Fatal("GetDefaultValidator() returned nil validator")
	}

	// Verify grace period is set correctly
	if validator.gracePeriodDays != 7 {
		t.Errorf("Expected grace period 7 days, got %d", validator.gracePeriodDays)
	}

	// Verify the embedded public key is valid by checking we can parse it
	if validator.publicKey == nil {
		t.Error("GetDefaultValidator() validator has nil public key")
	}

	// Test with different grace periods
	testCases := []int{0, 7, 14, 30}
	for _, days := range testCases {
		v, err := GetDefaultValidator(days)
		if err != nil {
			t.Errorf("GetDefaultValidator(%d) failed: %v", days, err)
		}
		if v.gracePeriodDays != days {
			t.Errorf("GetDefaultValidator(%d) grace period = %d, want %d", days, v.gracePeriodDays, days)
		}
	}
}

// Compact-licence tests were removed alongside the compact format
// itself when the agent went open-source. The HMAC secret that
// authenticated compact tokens could not survive a public source
// tree (see docs/LICENSE-SYSTEM.md for the JWT-only design that
// replaced it).
