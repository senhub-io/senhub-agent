package citrix

import (
	"context"
	"time"
)

// DeliveryControllerClient provides access to Citrix Delivery Controller APIs
type DeliveryControllerClient interface {
	// GetSites retrieves all sites from the Delivery Controller
	GetSites(ctx context.Context) ([]Site, error)
	
	// GetMachinesBySite retrieves all machines for a specific site
	GetMachinesBySite(ctx context.Context, siteName string) ([]string, error)
	
	// TestConnectivity tests the connection to the Delivery Controller
	TestConnectivity(ctx context.Context) error
}

// Site represents a Citrix site from CVAD API
type Site struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
}

// TokenResponse represents the authentication token response
type TokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// DeliveryControllerConfig contains configuration for the Delivery Controller client
type DeliveryControllerConfig struct {
	URL          string   `json:"url"`
	FallbackURLs []string `json:"fallback_urls"`
	SiteFilter   string   `json:"site_filter"`
	VerifySSL    bool     `json:"verify_ssl"`
	Timeout      time.Duration
}

// AuthConfig contains authentication configuration (shared between Director and DDC)
type AuthConfig struct {
	Method   string
	Username string
	Password string
}