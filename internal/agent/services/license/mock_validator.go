package license

import (
	"encoding/json"
	"fmt"
	"time"
)

// MockValidator is a simple validator for testing that doesn't require JWT signature verification
// It accepts simple JSON tokens for development and testing purposes
type MockValidator struct {
	gracePeriodDays int
}

// NewMockValidator creates a new mock validator for testing
func NewMockValidator(gracePeriodDays int) *MockValidator {
	return &MockValidator{
		gracePeriodDays: gracePeriodDays,
	}
}

// MockLicenseData represents a simple JSON license for testing
type MockLicenseData struct {
	Tier             string   `json:"tier"`
	AuthorizedProbes []string `json:"authorized_probes"`
	ExpiresAt        string   `json:"expires_at"` // ISO 8601 format
	IssuedAt         string   `json:"issued_at"`  // ISO 8601 format
	Subject          string   `json:"subject"`
}

// ValidateLicense validates a mock JSON license token
func (v *MockValidator) ValidateLicense(token string) (*License, error) {
	var data MockLicenseData
	if err := json.Unmarshal([]byte(token), &data); err != nil {
		return nil, fmt.Errorf("failed to parse mock license: %w", err)
	}

	// Parse timestamps
	expiresAt, err := time.Parse(time.RFC3339, data.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("invalid expires_at format: %w", err)
	}

	issuedAt, err := time.Parse(time.RFC3339, data.IssuedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid issued_at format: %w", err)
	}

	isExpired := time.Now().After(expiresAt)

	license := &License{
		Tier:             LicenseTier(data.Tier),
		AuthorizedProbes: data.AuthorizedProbes,
		ExpiresAt:        expiresAt,
		IssuedAt:         issuedAt,
		Subject:          data.Subject,
		IsExpired:        isExpired,
		GracePeriodDays:  v.gracePeriodDays,
	}

	return license, nil
}

// IsProbeAuthorized checks if a probe is authorized by the license
func (v *MockValidator) IsProbeAuthorized(license *License, probeName string) bool {
	// Check if probe is in free tier (always authorized)
	if isFreeTierProbe(probeName) {
		return true
	}

	// ONLINE MODE BYPASS: If license is nil, authorize all probes (Enterprise behavior)
	// This allows online mode agents to work without JWT license tokens
	// while offline mode agents require explicit licenses
	if license == nil {
		return true // ⬅️ CHANGED: nil license = Enterprise (all probes authorized)
	}

	// Check if expired and not in grace period
	if license.IsExpired && !v.IsInGracePeriod(license) {
		return false
	}

	// Check if probe is in authorized list
	for _, authorizedProbe := range license.AuthorizedProbes {
		if authorizedProbe == probeName || authorizedProbe == "*" {
			return true
		}
	}

	return false
}

// IsInGracePeriod checks if an expired license is still in grace period
func (v *MockValidator) IsInGracePeriod(license *License) bool {
	if !license.IsExpired {
		return false
	}

	gracePeriodEnd := license.ExpiresAt.Add(time.Duration(v.gracePeriodDays) * 24 * time.Hour)
	return time.Now().Before(gracePeriodEnd)
}
