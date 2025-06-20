package citrix

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/Azure/go-ntlmssp"
	"senhub-agent.go/internal/agent/services/logger"
)

// citrixClient implements the CitrixClient interface for OData API communication
type citrixClient struct {
	config     CitrixClientConfig
	httpClient *http.Client
	logger     *logger.ModuleLogger
	baseURL    string
}

// Ensure citrixClient implements CitrixClient interface
var _ CitrixClient = &citrixClient{}

// NewCitrixClient creates a new Citrix OData API client
func NewCitrixClient(config CitrixClientConfig, baseLogger *logger.Logger) (CitrixClient, error) {
	// Create module-specific logger for citrix client
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.client")

	// Normalize baseURL
	baseURL := strings.TrimSuffix(config.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/Data") {
		baseURL = baseURL + "/Data"
	}

	// Create HTTP client with TLS configuration
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !config.VerifySSL, // #nosec G402 - Configurable SSL verification for development/self-signed certificates
		},
	}

	// Configure authentication transport based on method
	var httpClient *http.Client
	switch AuthenticationMethod(config.AuthMethod) {
	case AuthMethodNTLM:
		// Use NTLM authentication transport
		httpClient = &http.Client{
			Transport: ntlmssp.Negotiator{
				RoundTripper: transport,
			},
			Timeout: config.Timeout,
		}
	case AuthMethodBasic:
		// Basic authentication will be handled in request headers
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
		}
	case AuthMethodKerberos:
		// Kerberos authentication - for future implementation
		return nil, fmt.Errorf("kerberos authentication not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported authentication method: %s", config.AuthMethod)
	}

	client := &citrixClient{
		config:     config,
		httpClient: httpClient,
		logger:     moduleLogger,
		baseURL:    baseURL,
	}

	return client, nil
}

// Connect establishes a connection to the Citrix OData API endpoint
func (c *citrixClient) Connect(ctx context.Context) error {
	c.logger.Info().
		Str("base_url", c.baseURL).
		Str("auth_method", c.config.AuthMethod).
		Msg("Connecting to Citrix OData API")

	// Test connection by performing a simple GET request to the service root
	testURL := c.baseURL
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %v", err)
	}

	// Add required Citrix headers
	c.addCitrixHeaders(req)

	// Add authentication headers
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Citrix OData API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("connection test failed with status %d: %s", resp.StatusCode, string(body))
	}

	c.logger.Info().
		Int("status_code", resp.StatusCode).
		Msg("Successfully connected to Citrix OData API")

	return nil
}

// Disconnect closes the connection
func (c *citrixClient) Disconnect(ctx context.Context) error {
	c.logger.Info().Msg("Disconnecting from Citrix OData API")
	// No explicit disconnect needed for HTTP client
	return nil
}

// GetSessions retrieves sessions data from the OData API
func (c *citrixClient) GetSessions(ctx context.Context, sinceTime time.Time) ([]Session, error) {
	endpoint := "/Sessions"
	filter := fmt.Sprintf("ModifiedDate ge %s", formatODataDateTime(sinceTime))
	
	var sessions []Session
	err := c.getODataCollection(ctx, endpoint, filter, &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions: %v", err)
	}

	c.logger.Debug().
		Int("session_count", len(sessions)).
		Time("since_time", sinceTime).
		Msg("Retrieved sessions from Citrix API")

	return sessions, nil
}

// GetMachines retrieves machines data from the OData API
func (c *citrixClient) GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	endpoint := "/Machines"
	filter := fmt.Sprintf("ModifiedDate ge %s", formatODataDateTime(sinceTime))
	
	var machines []Machine
	err := c.getODataCollection(ctx, endpoint, filter, &machines)
	if err != nil {
		return nil, fmt.Errorf("failed to get machines: %v", err)
	}

	c.logger.Debug().
		Int("machine_count", len(machines)).
		Time("since_time", sinceTime).
		Msg("Retrieved machines from Citrix API")

	return machines, nil
}

// GetDesktopGroups retrieves desktop groups data from the OData API
func (c *citrixClient) GetDesktopGroups(ctx context.Context) ([]DesktopGroup, error) {
	endpoint := "/DesktopGroups"
	
	var desktopGroups []DesktopGroup
	err := c.getODataCollection(ctx, endpoint, "", &desktopGroups)
	if err != nil {
		return nil, fmt.Errorf("failed to get desktop groups: %v", err)
	}

	c.logger.Debug().
		Int("desktop_group_count", len(desktopGroups)).
		Msg("Retrieved desktop groups from Citrix API")

	return desktopGroups, nil
}

// GetConnectionFailureLogs retrieves connection failure logs from the OData API
func (c *citrixClient) GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error) {
	endpoint := "/ConnectionFailureLogs"
	filter := fmt.Sprintf("FailureDate ge %s", formatODataDateTime(sinceTime))
	
	var failureLogs []ConnectionFailureLog
	err := c.getODataCollection(ctx, endpoint, filter, &failureLogs)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection failure logs: %v", err)
	}

	c.logger.Debug().
		Int("failure_log_count", len(failureLogs)).
		Time("since_time", sinceTime).
		Msg("Retrieved connection failure logs from Citrix API")

	return failureLogs, nil
}

// GetControllerStatus retrieves controller status information
func (c *citrixClient) GetControllerStatus(ctx context.Context) ([]Controller, error) {
	// Note: This endpoint may vary depending on Citrix version
	// This is a placeholder implementation
	endpoint := "/Controllers"
	
	var controllers []Controller
	err := c.getODataCollection(ctx, endpoint, "", &controllers)
	if err != nil {
		c.logger.Warn().
			Err(err).
			Msg("Failed to get controller status - endpoint may not be available in this Citrix version")
		// Return empty slice instead of error for controller status as it's optional
		return []Controller{}, nil
	}

	c.logger.Debug().
		Int("controller_count", len(controllers)).
		Msg("Retrieved controllers from Citrix API")

	return controllers, nil
}

// getODataCollection performs a GET request to an OData endpoint with pagination support
func (c *citrixClient) getODataCollection(ctx context.Context, endpoint, filter string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	params.Add("$top", "1000") // Pagination limit

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}

	// Handle pagination
	for url != "" {
		var response ODataResponse
		nextURL, err := c.performRequest(ctx, url, &response)
		if err != nil {
			return err
		}

		// Append items from this page
		allItems = append(allItems, response.Value...)

		// Check for next page
		url = nextURL
		if url != "" && !strings.HasPrefix(url, "http") {
			// Relative URL, make it absolute
			if strings.HasPrefix(url, "/") {
				url = c.baseURL + url
			} else {
				url = c.baseURL + "/" + url
			}
		}

		c.logger.Debug().
			Int("items_retrieved", len(response.Value)).
			Str("next_url", url).
			Msg("Retrieved page from OData endpoint")
	}

	// Convert to the expected type
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %v", err)
	}

	if err := json.Unmarshal(jsonData, result); err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// performRequest executes a single HTTP request with retry logic
func (c *citrixClient) performRequest(ctx context.Context, url string, result interface{}) (nextLink string, err error) {
	var lastErr error

	for attempt := 0; attempt < c.config.MaxRetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := time.Duration(float64(time.Second) * c.config.RetryBackoffFactor * float64(attempt))
			c.logger.Debug().
				Int("attempt", attempt+1).
				Dur("delay", delay).
				Msg("Retrying request after delay")
			
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		nextLink, lastErr = c.doRequest(ctx, url, result)
		if lastErr == nil {
			return nextLink, nil
		}

		c.logger.Warn().
			Err(lastErr).
			Int("attempt", attempt+1).
			Int("max_attempts", c.config.MaxRetryAttempts).
			Msg("Request failed, will retry if attempts remaining")
	}

	return "", fmt.Errorf("request failed after %d attempts: %v", c.config.MaxRetryAttempts, lastErr)
}

// doRequest performs a single HTTP request
func (c *citrixClient) doRequest(ctx context.Context, url string, result interface{}) (nextLink string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Add required Citrix headers
	c.addCitrixHeaders(req)

	// Add authentication headers
	c.addAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	// Parse the response
	var odataResp ODataResponse
	if err := json.Unmarshal(body, &odataResp); err != nil {
		return "", fmt.Errorf("failed to parse OData response: %v", err)
	}

	// Copy the response to result
	*result.(*ODataResponse) = odataResp

	return odataResp.NextLink, nil
}

// addCitrixHeaders adds required Citrix-specific headers
func (c *citrixClient) addCitrixHeaders(req *http.Request) {
	req.Header.Set("Citrix-CustomerId", "CitrixOnPremises")
	req.Header.Set("Citrix-InstanceId", c.config.Environment)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "SenHub-Citrix-Collector/1.0")
}

// addAuthHeaders adds authentication headers based on the configured method
func (c *citrixClient) addAuthHeaders(req *http.Request) {
	switch AuthenticationMethod(c.config.AuthMethod) {
	case AuthMethodBasic:
		req.SetBasicAuth(c.config.Username, c.config.Password)
	case AuthMethodNTLM:
		// NTLM authentication is handled by the transport layer
		req.SetBasicAuth(c.config.Username, c.config.Password)
	case AuthMethodKerberos:
		// Kerberos authentication - placeholder for future implementation
		c.logger.Warn().Msg("Kerberos authentication not yet implemented")
	}
}

// formatODataDateTime formats a time for OData filter queries
func formatODataDateTime(t time.Time) string {
	// OData datetime format: 2023-12-20T10:30:00Z
	return t.UTC().Format("2006-01-02T15:04:05Z")
}