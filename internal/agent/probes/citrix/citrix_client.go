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
		DisableKeepAlives: false, // Keep connections alive for NTLM
		MaxIdleConns:      10,
		IdleConnTimeout:   90 * time.Second,
	}

	// Configure authentication transport based on method
	var httpClient *http.Client
	switch AuthenticationMethod(config.AuthMethod) {
	case AuthMethodNTLM:
		// Use NTLM authentication transport with credentials
		// Parse domain\username format - handle both single and double backslash
		username := config.Username
		domain := ""
		
		// Handle domain\username format (can be single or double backslash in YAML)
		if strings.Contains(username, "\\") {
			// Handle both single backslash and double backslash cases
			separators := []string{"\\\\", "\\"}
			for _, sep := range separators {
				if strings.Contains(username, sep) {
					parts := strings.SplitN(username, sep, 2)
					if len(parts) == 2 {
						domain = parts[0]
						username = parts[1]
						break
					}
				}
			}
		}
		
		moduleLogger.Debug().
			Str("original_username", config.Username).
			Str("parsed_domain", domain).
			Str("parsed_username", username).
			Str("password_length", fmt.Sprintf("%d", len(config.Password))).
			Msg("Configuring NTLM authentication with parsed credentials")
		
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
		c.logger.Debug().
			Int("status_code", resp.StatusCode).
			Str("response_body", string(body)).
			Str("test_url", testURL).
			Msg("Connection test failed with error response")
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
	
	c.logger.Debug().
		Time("since_time", sinceTime).
		Str("filter", filter).
		Msg("Getting sessions with filter")
	
	var sessions []Session
	err := c.getODataCollectionUnlimited(ctx, endpoint, filter, &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions: %v", err)
	}

	c.logger.Debug().
		Int("session_count", len(sessions)).
		Time("since_time", sinceTime).
		Msg("Retrieved sessions from Citrix API")

	return sessions, nil
}

// GetSessionsByConnectionState retrieves sessions filtered by ConnectionState using OData filter
func (c *citrixClient) GetSessionsByConnectionState(ctx context.Context, connectionStates []int) ([]Session, error) {
	endpoint := "/Sessions"
	
	// Build OData filter for ConnectionState
	var stateFilters []string
	for _, state := range connectionStates {
		stateFilters = append(stateFilters, fmt.Sprintf("ConnectionState eq %d", state))
	}
	filter := strings.Join(stateFilters, " or ")
	
	c.logger.Debug().
		Ints("connection_states", connectionStates).
		Str("filter", filter).
		Msg("Getting sessions filtered by ConnectionState")
	
	var sessions []Session
	err := c.getODataCollectionUnlimited(ctx, endpoint, filter, &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions by connection state: %v", err)
	}

	c.logger.Debug().
		Int("session_count", len(sessions)).
		Ints("connection_states", connectionStates).
		Msg("Retrieved sessions filtered by ConnectionState from Citrix API")

	return sessions, nil
}

// GetMachines retrieves machines data from the OData API
func (c *citrixClient) GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	endpoint := "/Machines"
	
	var filter string
	if !sinceTime.IsZero() {
		filter = fmt.Sprintf("ModifiedDate ge %s", formatODataDateTime(sinceTime))
		c.logger.Debug().
			Time("since_time", sinceTime).
			Str("filter", filter).
			Msg("Getting machines with time filter")
	} else {
		// No filter - get all machines for infrastructure metrics
		c.logger.Debug().Msg("Getting all machines (no time filter)")
	}
	
	var machines []Machine
	err := c.getODataCollectionUnlimited(ctx, endpoint, filter, &machines)
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
	err := c.getODataCollectionUnlimited(ctx, endpoint, "", &desktopGroups)
	if err != nil {
		return nil, fmt.Errorf("failed to get desktop groups: %v", err)
	}

	c.logger.Info().
		Int("desktop_group_count", len(desktopGroups)).
		Msg("Retrieved desktop groups from Citrix API")
	
	// Log details of each desktop group for debugging
	for _, dg := range desktopGroups {
		c.logger.Debug().
			Str("desktop_group_id", dg.DesktopGroupId).
			Str("id", dg.Id).
			Str("effective_id", dg.GetEffectiveId()).
			Str("desktop_group_name", dg.Name).
			Bool("enabled", dg.Enabled).
			Msg("Desktop group details")
	}

	return desktopGroups, nil
}

// GetConnectionFailureLogs retrieves connection failure logs from the OData API
func (c *citrixClient) GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error) {
	endpoint := "/ConnectionFailureLogs"
	filter := fmt.Sprintf("FailureDate ge %s", formatODataDateTime(sinceTime))
	
	c.logger.Debug().
		Time("since_time", sinceTime).
		Str("filter", filter).
		Msg("Getting connection failure logs with filter")
	
	var failureLogs []ConnectionFailureLog
	err := c.getODataCollectionUnlimited(ctx, endpoint, filter, &failureLogs)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection failure logs: %v", err)
	}

	c.logger.Debug().
		Int("failure_log_count", len(failureLogs)).
		Time("since_time", sinceTime).
		Msg("Retrieved connection failure logs from Citrix API")

	return failureLogs, nil
}

// GetConnectionFailureLogsWithExpand retrieves connection failure logs with expanded data from the OData API
func (c *citrixClient) GetConnectionFailureLogsWithExpand(ctx context.Context, sinceTime time.Time, expand []string) ([]ConnectionFailureLog, error) {
	endpoint := "/ConnectionFailureLogs"
	filter := fmt.Sprintf("FailureDate ge %s", formatODataDateTime(sinceTime))
	
	// Build expand parameter
	expandParam := ""
	if len(expand) > 0 {
		expandParam = strings.Join(expand, ",")
	}
	
	c.logger.Debug().
		Time("since_time", sinceTime).
		Str("filter", filter).
		Str("expand", expandParam).
		Msg("Getting connection failure logs with filter and expand")
	
	var failureLogs []ConnectionFailureLog
	err := c.getODataCollectionWithExpand(ctx, endpoint, filter, expandParam, &failureLogs)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection failure logs with expand: %v", err)
	}

	c.logger.Debug().
		Int("failure_log_count", len(failureLogs)).
		Time("since_time", sinceTime).
		Str("expand", expandParam).
		Msg("Retrieved connection failure logs with expanded data from Citrix API")

	return failureLogs, nil
}

// GetConnectionFailureCategories retrieves connection failure category mappings from the OData API
func (c *citrixClient) GetConnectionFailureCategories(ctx context.Context) ([]ConnectionFailureCategory, error) {
	endpoint := "/ConnectionFailureCategories"
	
	c.logger.Debug().Msg("Getting connection failure categories")
	
	var categories []ConnectionFailureCategory
	err := c.getODataCollectionUnlimited(ctx, endpoint, "", &categories)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection failure categories: %v", err)
	}

	c.logger.Debug().
		Int("category_count", len(categories)).
		Msg("Retrieved connection failure categories from Citrix API")

	return categories, nil
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

// getODataCollectionUnlimited retrieves ALL items without pagination limits
func (c *citrixClient) getODataCollectionUnlimited(ctx context.Context, endpoint, filter string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters - remove $top limit to get all items
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	// No $top parameter = get all items

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}
	pageCount := 0

	// Handle pagination
	for url != "" {
		pageCount++
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
			Int("total_items", len(allItems)).
			Int("page_number", pageCount).
			Str("next_url", url).
			Msg("Retrieved unlimited page from OData endpoint")
	}

	c.logger.Info().
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Str("endpoint", endpoint).
		Msg("Completed unlimited OData collection retrieval")

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

// getODataCollectionWithExpand retrieves items with $expand parameter for related entities
func (c *citrixClient) getODataCollectionWithExpand(ctx context.Context, endpoint, filter, expand string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters including $expand
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	if expand != "" {
		params.Add("$expand", expand)
	}
	// No $top parameter = get all items

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}
	pageCount := 0

	// Handle pagination
	for url != "" {
		pageCount++
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
			Int("total_items", len(allItems)).
			Int("page_number", pageCount).
			Str("expand", expand).
			Str("next_url", url).
			Msg("Retrieved expanded page from OData endpoint")
	}

	c.logger.Info().
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Str("endpoint", endpoint).
		Str("expand", expand).
		Msg("Completed expanded OData collection retrieval")

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
	
	// Debug log the full request details
	c.logger.Debug().
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Str("host", req.Host).
		Interface("headers", req.Header).
		Msg("Sending HTTP request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("url", url).
			Str("auth_method", c.config.AuthMethod).
			Msg("HTTP request completely failed")
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	c.logger.Debug().
		Int("status_code", resp.StatusCode).
		Str("url", url).
		Interface("response_headers", resp.Header).
		Msg("Received HTTP response")

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		
		// Log authentication failures with more detail
		if resp.StatusCode == 401 {
			c.logger.Error().
				Int("status_code", resp.StatusCode).
				Str("response_body", string(body)).
				Str("url", url).
				Str("auth_method", c.config.AuthMethod).
				Str("username", c.config.Username).
				Interface("response_headers", resp.Header).
				Interface("request_headers", req.Header).
				Msg("Authentication failed - check credentials and authentication method")
		} else {
			c.logger.Debug().
				Int("status_code", resp.StatusCode).
				Str("response_body", string(body)).
				Str("url", url).
				Msg("HTTP request failed with error response")
		}
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
	// Essential headers for OData API
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "SenHub-Citrix-Collector/1.0")
	req.Header.Set("Content-Type", "application/json")
	
	// Some Citrix environments may require these headers
	if c.config.Environment != "" {
		req.Header.Set("Citrix-InstanceId", c.config.Environment)
	}
}

// addAuthHeaders adds authentication headers based on the configured method
func (c *citrixClient) addAuthHeaders(req *http.Request) {
	switch AuthenticationMethod(c.config.AuthMethod) {
	case AuthMethodBasic:
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.logger.Debug().
			Str("auth_method", "basic").
			Str("url", req.URL.Path).
			Str("username", c.config.Username).
			Msg("Added basic authentication headers")
	case AuthMethodNTLM:
		// NTLM authentication is handled by the transport layer
		// Set basic auth as fallback - many Citrix servers accept this for NTLM
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.logger.Debug().
			Str("auth_method", "ntlm_with_basic_fallback").
			Str("url", req.URL.Path).
			Str("username", c.config.Username).
			Str("username_length", fmt.Sprintf("%d", len(c.config.Username))).
			Str("password_length", fmt.Sprintf("%d", len(c.config.Password))).
			Msg("NTLM transport configured with basic auth fallback")
	case AuthMethodKerberos:
		// Kerberos authentication - placeholder for future implementation
		c.logger.Warn().Msg("Kerberos authentication not yet implemented")
	}
}

// GetDeliveryGroupById retrieves a specific delivery group by ID using OData filter
func (c *citrixClient) GetDeliveryGroupById(ctx context.Context, deliveryGroupId string) (*DesktopGroup, error) {
	// Validate the delivery group ID
	if deliveryGroupId == "" {
		return nil, fmt.Errorf("delivery group ID cannot be empty")
	}
	
	c.logger.Debug().
		Str("delivery_group_id", deliveryGroupId).
		Msg("Attempting to fetch specific delivery group by ID")
	
	// Use OData endpoint with specific filter for the ID
	// Use only Id field as DesktopGroupId doesn't exist on DesktopGroup type
	// Format as GUID for OData (no quotes, use guid syntax)
	endpoint := "/DesktopGroups"
	filter := fmt.Sprintf("Id eq guid'%s'", deliveryGroupId)
	
	var desktopGroups []DesktopGroup
	err := c.getODataCollection(ctx, endpoint, filter, &desktopGroups)
	if err != nil {
		c.logger.Debug().
			Err(err).
			Str("delivery_group_id", deliveryGroupId).
			Msg("Failed to get delivery group via OData filter")
		return nil, fmt.Errorf("failed to get delivery group by ID: %v", err)
	}
	
	if len(desktopGroups) == 0 {
		c.logger.Debug().
			Str("delivery_group_id", deliveryGroupId).
			Msg("No delivery group found with the specified ID")
		return nil, fmt.Errorf("delivery group with ID '%s' not found", deliveryGroupId)
	}
	
	c.logger.Debug().
		Str("delivery_group_id", deliveryGroupId).
		Str("delivery_group_name", desktopGroups[0].Name).
		Msg("Successfully retrieved delivery group by ID")
	
	return &desktopGroups[0], nil
}

// GetConnections retrieves connection details with logon breakdown metrics
func (c *citrixClient) GetConnections(ctx context.Context, sinceTime time.Time) ([]Connection, error) {
	endpoint := "/Connections"
	filter := ""
	
	// Add time filter if provided
	if !sinceTime.IsZero() {
		filter = fmt.Sprintf("LogOnStartDate gt %s", formatODataDateTime(sinceTime))
	}
	
	var connections []Connection
	err := c.getODataCollection(ctx, endpoint, filter, &connections)
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}
	
	c.logger.Debug().
		Int("connections_count", len(connections)).
		Time("since", sinceTime).
		Msg("Retrieved connections with logon breakdown metrics")
	
	return connections, nil
}

// formatODataDateTime formats a time for OData filter queries
func formatODataDateTime(t time.Time) string {
	// OData datetime format: 2023-12-20T10:30:00Z
	// Ensure we don't send future dates that might confuse the server
	now := time.Now().UTC()
	if t.After(now) {
		// Note: Future time requested, using current time instead
		t = now
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}