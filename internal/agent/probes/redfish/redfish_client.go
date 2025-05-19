package redfish

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// RedfishClient provides a client for interacting with Redfish API endpoints
type RedfishClient struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	authToken  string
	sessionURL string
	logger     *logger.Logger
	mu         sync.Mutex // Mutex for concurrent access to client
}

// Ensure RedfishClient implements RedfishClientInterface
var _ RedfishClientInterface = &RedfishClient{}

// RedfishResponse encapsulates common Redfish response fields
type RedfishResponse struct {
	OdataContext      string                 `json:"@odata.context,omitempty"`
	OdataID           string                 `json:"@odata.id,omitempty"`
	OdataType         string                 `json:"@odata.type,omitempty"`
	ID                string                 `json:"Id,omitempty"`
	Name              string                 `json:"Name,omitempty"`
	Description       string                 `json:"Description,omitempty"`
	Status            *Status                `json:"Status,omitempty"`
	Members           []map[string]string    `json:"Members,omitempty"`
	// Raw contains the raw JSON data
	Raw               json.RawMessage
	MembersCount      int                    `json:"Members@odata.count,omitempty"`
	Oem               map[string]interface{} `json:"Oem,omitempty"`
	Manufacturer      string                 `json:"Manufacturer,omitempty"`
	Model             string                 `json:"Model,omitempty"`
	SerialNumber      string                 `json:"SerialNumber,omitempty"`
	FirmwareVersion   string                 `json:"FirmwareVersion,omitempty"`
	PartNumber        string                 `json:"PartNumber,omitempty"`
	PowerState        string                 `json:"PowerState,omitempty"`
	SKU               string                 `json:"SKU,omitempty"`
	UUID              string                 `json:"UUID,omitempty"`
	AssetTag          string                 `json:"AssetTag,omitempty"`
	BiosVersion       string                 `json:"BiosVersion,omitempty"`
	SystemType        string                 `json:"SystemType,omitempty"`
	ProcessorSummary  map[string]interface{} `json:"ProcessorSummary,omitempty"`
	MemorySummary     map[string]interface{} `json:"MemorySummary,omitempty"`
	Storage           map[string]interface{} `json:"Storage,omitempty"`
	Processors        map[string]interface{} `json:"Processors,omitempty"`
	Memory            map[string]interface{} `json:"Memory,omitempty"`
	EthernetInterfaces map[string]interface{} `json:"EthernetInterfaces,omitempty"`
	Links             map[string]interface{} `json:"Links,omitempty"`
	Actions           map[string]interface{} `json:"Actions,omitempty"`
	Temperatures      []map[string]interface{} `json:"Temperatures,omitempty"`
	Fans              []map[string]interface{} `json:"Fans,omitempty"`
	Voltages          []map[string]interface{} `json:"Voltages,omitempty"`
	PowerSupplies     []map[string]interface{} `json:"PowerSupplies,omitempty"`
	PowerControl      []map[string]interface{} `json:"PowerControl,omitempty"`
	StorageControllers []map[string]interface{} `json:"StorageControllers,omitempty"`
}

// Status represents the common Redfish Status object
type Status struct {
	State        string `json:"State,omitempty"`
	Health       string `json:"Health,omitempty"`
	HealthRollup string `json:"HealthRollup,omitempty"`
}

// NewRedfishClient creates a new Redfish API client
func NewRedfishClient(baseURL, username, password string, logger *logger.Logger, verifySSL bool) (*RedfishClient, error) {
	// Normalize baseURL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL + "/"
	}
	if !strings.HasSuffix(baseURL, "redfish/v1/") {
		if strings.HasSuffix(baseURL, "redfish/") {
			baseURL = baseURL + "v1/"
		} else {
			baseURL = baseURL + "redfish/v1/"
		}
	}

	// Check if the baseURL is valid
	_, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid baseURL: %v", err)
	}

	// Configure transport with reasonable timeouts
	transport := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
	}
	
	// Skip TLS verification if requested
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		logger.Info().Str("endpoint", baseURL).Msg("TLS certificate verification disabled")
	}

	// Create HTTP client with configured transport
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	return &RedfishClient{
		baseURL:    baseURL,
		username:   username,
		password:   password,
		httpClient: httpClient,
		logger:     logger,
	}, nil
}

// Connect establishes a session with the Redfish API
func (c *RedfishClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build session URL
	sessionURL := c.baseURL + "SessionService/Sessions"

	// Create session payload
	session := map[string]string{
		"UserName": c.username,
		"Password": c.password,
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("error marshaling session payload: %v", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", sessionURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("error creating session request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error creating session: %v", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Debug().
			Int("status_code", resp.StatusCode).
			Str("response", string(body)).
			Msg("Failed to create session")
		
		// If session creation fails, try basic auth
		c.logger.Info().Msg("Session auth failed, falling back to basic auth")
		return nil
	}

	// Extract token from response
	c.authToken = resp.Header.Get("X-Auth-Token")
	c.sessionURL = resp.Header.Get("Location")

	// If no X-Auth-Token is returned, try to extract from response body
	if c.authToken == "" {
		var sessionResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err == nil {
			if token, ok := sessionResp["Token"].(string); ok {
				c.authToken = token
			}
		}
	}

	c.logger.Info().
		Str("endpoint", c.baseURL).
		Msg("Successfully connected to Redfish API")

	return nil
}

// Disconnect closes the session with the Redfish API
func (c *RedfishClient) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If no session is established, nothing to do
	if c.authToken == "" || c.sessionURL == "" {
		return nil
	}

	// Create request to delete session
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.sessionURL, nil)
	if err != nil {
		return fmt.Errorf("error creating delete session request: %v", err)
	}

	// Add auth token
	req.Header.Set("X-Auth-Token", c.authToken)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error deleting session: %v", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error deleting session: %s", string(body))
	}

	// Clear session data
	c.authToken = ""
	c.sessionURL = ""

	return nil
}

// Get performs a GET request to the specified path
func (c *RedfishClient) Get(ctx context.Context, path string) (*RedfishResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build URL
	requestURL := c.baseURL
	// Check if path is a full URL
	if strings.HasPrefix(path, "http") {
		requestURL = path
	} else {
		// Remove redundant /redfish/v1/ prefix if present
		if strings.HasPrefix(path, "/redfish/v1/") {
			path = strings.TrimPrefix(path, "/redfish/v1/")
		} else if strings.HasPrefix(path, "redfish/v1/") {
			path = strings.TrimPrefix(path, "redfish/v1/")
		}

		// Add path to baseURL
		if !strings.HasPrefix(path, "/") {
			requestURL += path
		} else {
			requestURL += strings.TrimPrefix(path, "/")
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GET request: %v", err)
	}

	// Add headers
	req.Header.Set("Accept", "application/json")
	
	// Add auth token if available, otherwise use basic auth
	if c.authToken != "" {
		req.Header.Set("X-Auth-Token", c.authToken)
	} else {
		req.SetBasicAuth(c.username, c.password)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending GET request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error response from %s: %d, %s", requestURL, resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	// Parse response
	var redfishResp RedfishResponse
	if err := json.Unmarshal(body, &redfishResp); err != nil {
		return nil, fmt.Errorf("error parsing response: %v", err)
	}
	
	// Store the raw JSON data
	redfishResp.Raw = body

	return &redfishResp, nil
}

// GetRaw performs a GET request and returns the raw response body
func (c *RedfishClient) GetRaw(ctx context.Context, path string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build URL
	requestURL := c.baseURL
	// Check if path is a full URL
	if strings.HasPrefix(path, "http") {
		requestURL = path
	} else {
		// Remove redundant /redfish/v1/ prefix if present
		if strings.HasPrefix(path, "/redfish/v1/") {
			path = strings.TrimPrefix(path, "/redfish/v1/")
		} else if strings.HasPrefix(path, "redfish/v1/") {
			path = strings.TrimPrefix(path, "redfish/v1/")
		}

		// Add path to baseURL
		if !strings.HasPrefix(path, "/") {
			requestURL += path
		} else {
			requestURL += strings.TrimPrefix(path, "/")
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GET request: %v", err)
	}

	// Add headers
	req.Header.Set("Accept", "application/json")
	
	// Add auth token if available, otherwise use basic auth
	if c.authToken != "" {
		req.Header.Set("X-Auth-Token", c.authToken)
	} else {
		req.SetBasicAuth(c.username, c.password)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending GET request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error response from %s: %d, %s", requestURL, resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	return body, nil
}

// RedfishVersionInfo contains information about Redfish API versions
type RedfishVersionInfo struct {
	// The main Redfish API version (e.g., "1.6.0")
	RedfishVersion string

	// Map of schema names to their versions
	SchemaVersions map[string]string

	// OEM-specific version information
	OemVersions map[string]interface{}
}

// DetectRedfishVersions retrieves the Redfish and schema versions
func (c *RedfishClient) DetectRedfishVersions(ctx context.Context) (*RedfishVersionInfo, error) {
	// Get the service root document
	resp, err := c.GetRaw(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get Redfish service root: %v", err)
	}

	var rootObj struct {
		RedfishVersion string `json:"RedfishVersion"`
		UUID           string `json:"UUID"`
		Name           string `json:"Name"`

		// Schema version information
		Links struct {
			Sessions struct {
				OdataID string `json:"@odata.id"`
			} `json:"Sessions"`
		} `json:"Links"`

		// OEM-specific version information
		Oem map[string]interface{} `json:"Oem"`

		// Schema annotations
		OdataContext string `json:"@odata.context"`
		OdataID      string `json:"@odata.id"`
		OdataType    string `json:"@odata.type"`
	}

	if err := json.Unmarshal(resp, &rootObj); err != nil {
		return nil, fmt.Errorf("failed to parse Redfish service root: %v", err)
	}

	result := &RedfishVersionInfo{
		RedfishVersion: rootObj.RedfishVersion,
		SchemaVersions: make(map[string]string),
		OemVersions:    make(map[string]interface{}),
	}

	// Extract schema versions from @odata.type
	if rootObj.OdataType != "" {
		// @odata.type typically looks like "#ServiceRoot.v1_5_0.ServiceRoot"
		parts := strings.Split(rootObj.OdataType, ".")
		if len(parts) >= 2 {
			// Extract the schema name and version
			schemaName := strings.TrimPrefix(parts[0], "#")
			schemaVersion := parts[1]
			result.SchemaVersions[schemaName] = schemaVersion
		}
	}

	// Store OEM-specific information if available
	if rootObj.Oem != nil {
		result.OemVersions = rootObj.Oem
	}

	return result, nil
}