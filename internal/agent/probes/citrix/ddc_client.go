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

	"senhub-agent.go/internal/agent/services/logger"
)

// deliveryControllerClient implements the DeliveryControllerClient interface
type deliveryControllerClient struct {
	config         DeliveryControllerConfig
	httpClient     *http.Client
	logger         *logger.ModuleLogger
	primaryURL     string
	fallbackURLs   []string
	authConfig     AuthConfig
	token          string
	tokenExpiry    time.Time
}

// NewDeliveryControllerClient creates a new Delivery Controller client
func NewDeliveryControllerClient(config DeliveryControllerConfig, authConfig AuthConfig, baseLogger *logger.Logger) (DeliveryControllerClient, error) {
	// Create module-specific logger
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.ddc")
	
	// Create HTTP client with TLS configuration
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !config.VerifySSL, // #nosec G402 - Configurable SSL verification
		},
		MaxIdleConns:      10,
		IdleConnTimeout:   90 * time.Second,
	}
	
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}
	
	return &deliveryControllerClient{
		config:       config,
		httpClient:   httpClient,
		logger:       moduleLogger,
		primaryURL:   strings.TrimSuffix(config.URL, "/"),
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
	
	urls := append([]string{c.primaryURL}, c.fallbackURLs...)
	
	var lastErr error
	for _, baseURL := range urls {
		url := fmt.Sprintf("%s/cvad/manage/Tokens", strings.TrimSuffix(baseURL, "/"))
		
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
		defer resp.Body.Close()
		
		body, _ := io.ReadAll(resp.Body)
		
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
		
		c.logger.Info().
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
	// Ensure we have a valid token
	if err := c.getToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	
	urls := append([]string{c.primaryURL}, c.fallbackURLs...)
	
	var lastErr error
	for _, baseURL := range urls {
		url := fmt.Sprintf("%s%s", strings.TrimSuffix(baseURL, "/"), endpoint)
		
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
			lastErr = err
			continue
		}
		
		// Set headers for CVAD API requests
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		// CVAD uses CWSAuth Bearer format instead of standard Bearer
		req.Header.Set("Authorization", fmt.Sprintf("CWSAuth Bearer=%s", c.token))
		// Add Citrix-CustomerId header (may be required for some operations)
		req.Header.Set("Citrix-CustomerId", "CitrixOnPremises")
		
		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logger.Warn().
				Err(err).
				Str("url", url).
				Msg("Request failed, trying next controller")
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}
		
		// Check for token expiry
		if resp.StatusCode == http.StatusUnauthorized {
			c.logger.Debug().Msg("Token expired, refreshing")
			c.token = ""
			// Retry with new token
			if err := c.getToken(ctx); err != nil {
				lastErr = err
				continue
			}
			// Retry the request once
			continue
		}
		
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
			c.logger.Warn().
				Int("status", resp.StatusCode).
				Str("url", url).
				Str("response", string(respBody)).
				Msg("Request failed")
			continue
		}
		
		c.logger.Debug().
			Str("url", url).
			Str("method", method).
			Msg("Request successful")
		
		return respBody, nil
	}
	
	return nil, fmt.Errorf("all controllers failed: %w", lastErr)
}