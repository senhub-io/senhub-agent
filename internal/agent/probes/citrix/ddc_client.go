package citrix

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/probesdk/logger"
)

// deliveryControllerClient implements the DeliveryControllerClient interface
type deliveryControllerClient struct {
	config       DeliveryControllerConfig
	httpClient   *http.Client
	logger       *logger.ModuleLogger
	primaryURL   string
	activeURL    string // Currently used URL (primary or fallback)
	fallbackURLs []string
	authConfig   AuthConfig
	token        string
	tokenExpiry  time.Time

	// Cached site info to avoid redundant GetMe calls per collection cycle
	cachedSiteID   string
	cachedSiteName string
	siteIDCachedAt time.Time
}

// siteIDCacheTTL is how long we cache the site ID from GetMe (sites don't change often)
const siteIDCacheTTL = 10 * time.Minute

// NewDeliveryControllerClient creates a new Delivery Controller client
func NewDeliveryControllerClient(config DeliveryControllerConfig, authConfig AuthConfig, baseLogger *logger.Logger) (DeliveryControllerClient, error) {
	// Create module-specific logger
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.ddc")

	// Create HTTP client with TLS configuration
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !config.VerifySSL, // #nosec G402 - Configurable SSL verification
		},
		MaxIdleConns:    10,
		IdleConnTimeout: 90 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	primary := strings.TrimSuffix(config.URL, "/")
	return &deliveryControllerClient{
		config:       config,
		httpClient:   httpClient,
		logger:       moduleLogger,
		primaryURL:   primary,
		activeURL:    primary,
		fallbackURLs: config.FallbackURLs,
		authConfig:   authConfig,
	}, nil
}

// CVADTokenResponse represents the CVAD token response
type CVADTokenResponse struct {
	Token      string    `json:"Token"`
	Principal  string    `json:"Principal"`
	UserId     string    `json:"UserId"`
	CustomerId string    `json:"CustomerId"`
	ExpiresAt  time.Time `json:"ExpiresAt"`
}

// getToken retrieves or refreshes the authentication token using CVAD format
func (c *deliveryControllerClient) getToken(ctx context.Context) error {
	// Check if we have a valid token
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-5*time.Minute)) {
		return nil
	}

	c.logger.Debug().Msg("Getting new CVAD authentication token")

	// Build URL list: active first, then others as fallback
	urls := []string{c.activeURL}
	if c.activeURL != c.primaryURL {
		urls = append(urls, c.primaryURL)
	}
	for _, fb := range c.fallbackURLs {
		fb = strings.TrimSuffix(fb, "/")
		if fb != c.activeURL && fb != c.primaryURL {
			urls = append(urls, fb)
		}
	}

	var lastErr error
	for _, baseURL := range urls {
		url := fmt.Sprintf("%s/cvad/manage/Tokens", baseURL)

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
		if err != nil {
			lastErr = err
			continue
		}

		// Set headers for CVAD authentication
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Basic auth for token request (username can be DOMAIN\username format)
		req.SetBasicAuth(c.authConfig.Username, c.authConfig.Password)

		c.logger.Debug().
			Str("url", url).
			Str("username", c.authConfig.Username).
			Msg("Requesting CVAD token")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("url", url).
				Msg("Failed to get token from Delivery Controller")
			lastErr = err
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			c.logger.Warn().
				Int("status", resp.StatusCode).
				Str("url", url).
				Str("response", string(body)).
				Msg("Failed to authenticate with Delivery Controller")
			continue
		}

		// Parse CVAD token response
		var tokenResp CVADTokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			lastErr = fmt.Errorf("failed to parse CVAD token response: %w", err)
			c.logger.Warn().
				Err(err).
				Str("response", string(body)).
				Msg("Failed to unmarshal token response")
			continue
		}

		// Store token information
		c.token = tokenResp.Token
		c.tokenExpiry = tokenResp.ExpiresAt

		c.logger.Debug().
			Str("principal", tokenResp.Principal).
			Str("user_id", tokenResp.UserId).
			Str("customer_id", tokenResp.CustomerId).
			Time("expires_at", c.tokenExpiry).
			Msg("Successfully obtained CVAD authentication token")

		return nil
	}

	return fmt.Errorf("failed to get token from all controllers: %w", lastErr)
}

// makeRequest performs an authenticated HTTP request
func (c *deliveryControllerClient) makeRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, error) {
	return c.makeRequestWithSiteID(ctx, method, endpoint, body, "")
}

// makeRequestWithSiteID performs an authenticated HTTP request with optional site ID header.
// Uses the active URL with recovery to primary and failover to fallbacks.
func (c *deliveryControllerClient) makeRequestWithSiteID(ctx context.Context, method, endpoint string, body interface{}, siteID string) ([]byte, error) {
	if err := c.getToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Recovery: if on fallback, try primary first
	if c.activeURL != c.primaryURL {
		if respBody, err := c.doControllerRequest(ctx, method, c.primaryURL, endpoint, body, siteID); err == nil {
			c.activeURL = c.primaryURL
			c.logger.Info().
				Str("primary_url", c.primaryURL).
				Msg("Primary DDC recovered, switching back")
			return respBody, nil
		}
	}

	// Try active URL
	respBody, err := c.doControllerRequest(ctx, method, c.activeURL, endpoint, body, siteID)
	if err == nil {
		return respBody, nil
	}

	c.logger.Debug().Err(err).Str("url", c.activeURL).Msg("Active DDC request failed")

	// Failover: try fallbacks
	for _, fbURL := range c.fallbackURLs {
		fb := strings.TrimSuffix(fbURL, "/")
		if fb == c.activeURL {
			continue
		}

		respBody, err := c.doControllerRequest(ctx, method, fb, endpoint, body, siteID)
		if err == nil {
			c.logger.Warn().
				Str("failed_url", c.activeURL).
				Str("new_active_url", fb).
				Msg("DDC failover successful")
			c.activeURL = fb
			return respBody, nil
		}
	}

	return nil, fmt.Errorf("all DDC controllers unreachable: %w", err)
}

// doControllerRequest performs a single HTTP request to a specific controller URL
func (c *deliveryControllerClient) doControllerRequest(ctx context.Context, method, baseURL, endpoint string, body interface{}, siteID string) ([]byte, error) {
	url := fmt.Sprintf("%s%s", baseURL, endpoint)

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("CWSAuth Bearer=%s", c.token))
	req.Header.Set("Citrix-CustomerId", "CitrixOnPremises")
	if siteID != "" {
		req.Header.Set("Citrix-InstanceId", siteID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// Handle token expiry
	if resp.StatusCode == http.StatusUnauthorized {
		c.logger.Debug().Msg("Token expired, refreshing")
		c.token = ""
		if err := c.getToken(ctx); err != nil {
			return nil, err
		}
		// Retry once with new token
		return c.doControllerRequest(ctx, method, baseURL, endpoint, body, siteID)
	}

	if resp.StatusCode >= 400 {
		logLevel := c.logger.Warn()
		if resp.StatusCode == 404 && (strings.Contains(url, "/Controllers") || strings.Contains(url, "/Applications")) {
			logLevel = c.logger.Debug()
		}
		logLevel.Int("status", resp.StatusCode).Str("url", url).Msg("DDC request failed")
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	c.logger.Debug().Str("url", url).Str("method", method).Msg("DDC request successful")
	return respBody, nil
}

// getSiteInfo returns the cached site ID and name, refreshing from GetMe if stale.
// This avoids redundant /cvad/manage/me API calls during a single collection cycle.
func (c *deliveryControllerClient) getSiteInfo(ctx context.Context, requestedSiteName string) (siteID, siteName string, err error) {
	if c.cachedSiteID != "" && time.Since(c.siteIDCachedAt) < siteIDCacheTTL {
		if requestedSiteName != "" && c.cachedSiteName != requestedSiteName {
			c.logger.Warn().
				Str("requested_site", requestedSiteName).
				Str("user_site", c.cachedSiteName).
				Msg("User requesting different site than their own - using user's site for security")
		}
		return c.cachedSiteID, c.cachedSiteName, nil
	}

	meResp, err := c.GetMe(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get user info: %w", err)
	}

	if len(meResp.Customers) == 0 || len(meResp.Customers[0].Sites) == 0 {
		return "", "", fmt.Errorf("user has no accessible sites")
	}

	c.cachedSiteID = meResp.Customers[0].Sites[0].Id
	c.cachedSiteName = meResp.Customers[0].Sites[0].Name
	c.siteIDCachedAt = time.Now()

	if c.cachedSiteID == "" {
		return "", "", fmt.Errorf("user site ID not available")
	}

	if requestedSiteName != "" && c.cachedSiteName != requestedSiteName {
		c.logger.Warn().
			Str("requested_site", requestedSiteName).
			Str("user_site", c.cachedSiteName).
			Msg("User requesting different site than their own - using user's site for security")
	}

	return c.cachedSiteID, c.cachedSiteName, nil
}
