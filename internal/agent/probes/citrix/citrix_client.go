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
	config           CitrixClientConfig
	httpClient       *http.Client
	logger           *logger.ModuleLogger
	baseURL          string
	validMachineDNS  []string // DNS names from CVAD inventory for filtering
}

// Ensure citrixClient implements CitrixClient interface
var _ CitrixClient = &citrixClient{}

// NewCitrixClient creates a new Citrix OData API client
func NewCitrixClient(config CitrixClientConfig, baseLogger *logger.Logger) (CitrixClient, error) {
	// Create module-specific logger for citrix client
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.client")

	// Normalize baseURL and add Citrix Monitor OData path if not present
	baseURL := strings.TrimSuffix(config.BaseURL, "/")

	// Add complete Citrix Monitor OData API path if not already present
	if !strings.Contains(baseURL, "/Citrix/Monitor/OData") {
		baseURL = baseURL + "/Citrix/Monitor/OData/v4/Data"
	} else if !strings.HasSuffix(baseURL, "/Data") {
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
	// Use $expand=Machine to get machine details for filtering
	err := c.getODataCollectionUnlimitedWithExpand(ctx, endpoint, filter, "Machine", &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions: %v", err)
	}

	c.logger.Debug().
		Int("session_count", len(sessions)).
		Time("since_time", sinceTime).
		Msg("Retrieved sessions from Citrix API")

	// Apply client-side DNS filtering if we have a valid machine list
	if len(c.validMachineDNS) > 0 {
		sessions = c.filterSessionsByMachineDNS(sessions, c.validMachineDNS)
	}

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
	// Use $expand=Machine to get machine details for filtering
	err := c.getODataCollectionUnlimitedWithExpand(ctx, endpoint, filter, "Machine", &sessions)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions by connection state: %v", err)
	}

	c.logger.Debug().
		Int("session_count", len(sessions)).
		Ints("connection_states", connectionStates).
		Msg("Retrieved sessions filtered by ConnectionState from Citrix API")

	// Apply client-side DNS filtering if we have a valid machine list
	if len(c.validMachineDNS) > 0 {
		sessions = c.filterSessionsByMachineDNS(sessions, c.validMachineDNS)
	}

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

// GetMachinesFiltered retrieves machines filtered by DNS names using OData filters
func (c *citrixClient) GetMachinesFiltered(ctx context.Context, sinceTime time.Time, dnsNames []string) ([]Machine, error) {
	if len(dnsNames) == 0 {
		c.logger.Debug().Msg("No DNS names provided for filtering - using unfiltered query")
		return c.GetMachines(ctx, sinceTime)
	}
	
	endpoint := "/Machines"
	
	// Build DNS name filter
	var dnsFilters []string
	for _, dnsName := range dnsNames {
		if dnsName != "" {
			dnsFilters = append(dnsFilters, fmt.Sprintf("DnsName eq '%s'", strings.ReplaceAll(dnsName, "'", "''")))
		}
	}
	
	if len(dnsFilters) == 0 {
		c.logger.Debug().Msg("All DNS names were empty - using unfiltered query")
		return c.GetMachines(ctx, sinceTime)
	}
	
	var combinedFilter string
	
	// Combine DNS filter with time filter if provided
	dnsFilter := strings.Join(dnsFilters, " or ")
	if len(dnsFilters) > 1 {
		dnsFilter = fmt.Sprintf("(%s)", dnsFilter)
	}
	
	if !sinceTime.IsZero() {
		timeFilter := fmt.Sprintf("ModifiedDate ge %s", formatODataDateTime(sinceTime))
		combinedFilter = fmt.Sprintf("%s and %s", timeFilter, dnsFilter)
	} else {
		combinedFilter = dnsFilter
	}
	
	c.logger.Debug().
		Int("dns_names_count", len(dnsNames)).
		Int("valid_dns_filters", len(dnsFilters)).
		Str("filter", combinedFilter).
		Msg("Getting machines with DNS name filtering")
	
	var machines []Machine
	err := c.getODataCollectionUnlimited(ctx, endpoint, combinedFilter, &machines)
	if err != nil {
		// If filtering fails, fall back to unfiltered query and filter client-side
		c.logger.Debug().
			Err(err).
			Int("dns_names_count", len(dnsNames)).
			Msg("OData filtering failed (expected with >100 machines) - falling back to client-side filtering")
		
		allMachines, fallbackErr := c.GetMachines(ctx, sinceTime)
		if fallbackErr != nil {
			return nil, fmt.Errorf("both filtered and fallback queries failed: %v, %v", err, fallbackErr)
		}
		
		// Filter client-side
		dnsMap := make(map[string]bool)
		var validDnsNames []string
		var duplicateCount int
		
		for _, dnsName := range dnsNames {
			if dnsName != "" {
				if dnsMap[dnsName] {
					duplicateCount++
					c.logger.Warn().
						Str("duplicate_dns", dnsName).
						Msg("🔥 DUPLICATE DNS name found in CVAD inventory!")
				} else {
					dnsMap[dnsName] = true
					validDnsNames = append(validDnsNames, dnsName)
				}
			}
		}
		
		if duplicateCount > 0 {
			c.logger.Warn().
				Int("duplicate_dns_count", duplicateCount).
				Int("unique_dns_count", len(validDnsNames)).
				Int("total_dns_input", len(dnsNames)).
				Msg("🚨 Found duplicate DNS names in CVAD inventory filter")
		}
		
		// Log sample of CVAD DNS names for debugging
		if len(validDnsNames) > 0 {
			sampleSize := 5
			if len(validDnsNames) < sampleSize {
				sampleSize = len(validDnsNames)
			}
			c.logger.Info().
				Strs("cvad_sample_dns", validDnsNames[:sampleSize]).
				Int("total_cvad_dns", len(validDnsNames)).
				Msg("📋 Sample of CVAD inventory DNS names used for filtering")
		}
		
		c.logger.Info().
			Int("cvad_dns_names", len(dnsNames)).
			Int("odata_machines", len(allMachines)).
			Msg("Starting client-side DNS filtering - detailed analysis")
		
		var filteredMachines []Machine
		var matchedDNSNames []string
		var notMatchedCount int
		processedDNS := make(map[string]bool) // Track processed DNS to detect duplicates in OData
		machinesByDNS := make(map[string]Machine) // Store first machine by DNS for comparison
		
		var emptyDnsCount int
		for _, machine := range allMachines {
			if machine.DnsName == "" {
				emptyDnsCount++
				c.logger.Debug().
					Str("machine_name", machine.MachineName).
					Str("machine_id", machine.MachineId).
					Msg("OData machine has EMPTY DNS name - skipping")
				continue
			}
			
			if dnsMap[machine.DnsName] {
				// Check if we already processed this DNS from OData side
				if processedDNS[machine.DnsName] {
					currentMachine := machinesByDNS[machine.DnsName]
					
					// Intelligent machine selection: choose the better machine based on criteria
					shouldReplace := false
					reason := ""
					
					// Priority 1: Registration State (1=Registered is best, 0=Unregistered, 2=Error)
					if machine.RegistrationState == 1 && currentMachine.RegistrationState != 1 {
						shouldReplace = true
						reason = "better_registration_state"
					} else if machine.RegistrationState != 1 && currentMachine.RegistrationState == 1 {
						shouldReplace = false
						reason = "worse_registration_state"
					} else {
						// Priority 2: Fault State (lower is better - 0=no fault)
						if machine.FaultState < currentMachine.FaultState {
							shouldReplace = true
							reason = "better_fault_state"
						} else if machine.FaultState > currentMachine.FaultState {
							shouldReplace = false
							reason = "worse_fault_state"
						} else {
							// Priority 3: Last Connection Time (more recent is better)
							if machine.LastConnectionTime != nil && currentMachine.LastConnectionTime != nil {
								if machine.LastConnectionTime.After(*currentMachine.LastConnectionTime) {
									shouldReplace = true
									reason = "more_recent_connection"
								} else {
									shouldReplace = false
									reason = "older_connection"
								}
							} else if machine.LastConnectionTime != nil && currentMachine.LastConnectionTime == nil {
								shouldReplace = true
								reason = "has_connection_time"
							} else {
								shouldReplace = false
								reason = "same_criteria_keep_first"
							}
						}
					}
					
					currentConnectionStr := "null"
					if currentMachine.LastConnectionTime != nil {
						currentConnectionStr = currentMachine.LastConnectionTime.Format(time.RFC3339)
					}
					newConnectionStr := "null"
					if machine.LastConnectionTime != nil {
						newConnectionStr = machine.LastConnectionTime.Format(time.RFC3339)
					}
					
					c.logger.Info().
						Str("dns_name", machine.DnsName).
						Str("current_machine_id", currentMachine.MachineId).
						Int("current_registration_state", currentMachine.RegistrationState).
						Int("current_fault_state", currentMachine.FaultState).
						Str("current_last_connection", currentConnectionStr).
						Str("new_machine_id", machine.MachineId).
						Int("new_registration_state", machine.RegistrationState).
						Int("new_fault_state", machine.FaultState).
						Str("new_last_connection", newConnectionStr).
						Bool("will_replace", shouldReplace).
						Str("reason", reason).
						Msg("🔄 DUPLICATE OData machines - intelligent selection")
					
					if shouldReplace {
						// Replace the current machine in filteredMachines
						for i, filteredMachine := range filteredMachines {
							if filteredMachine.MachineId == currentMachine.MachineId {
								filteredMachines[i] = machine
								break
							}
						}
						machinesByDNS[machine.DnsName] = machine
						c.logger.Info().
							Str("dns_name", machine.DnsName).
							Str("replaced_id", currentMachine.MachineId).
							Str("new_id", machine.MachineId).
							Str("reason", reason).
							Msg("✅ Replaced machine with better candidate")
					}
					
					continue // Don't add to filteredMachines again (either kept current or replaced)
				}
				processedDNS[machine.DnsName] = true
				machinesByDNS[machine.DnsName] = machine // Store for comparison
				
				filteredMachines = append(filteredMachines, machine)
				matchedDNSNames = append(matchedDNSNames, machine.DnsName)
			} else {
				notMatchedCount++
				c.logger.Info().
					Str("machine_dns", machine.DnsName).
					Str("machine_name", machine.MachineName).
					Str("machine_id", machine.MachineId).
					Int("registration_state", machine.RegistrationState).
					Int("fault_state", machine.FaultState).
					Msg("🚫 OData machine NOT in CVAD inventory filter")
			}
		}
		
		c.logger.Info().
			Int("total_machines", len(allMachines)).
			Int("filtered_machines", len(filteredMachines)).
			Int("not_matched", notMatchedCount).
			Int("empty_dns", emptyDnsCount).
			Msg("Applied client-side DNS filtering")
			
		// Log first few matched DNS names for verification
		if len(matchedDNSNames) > 0 {
			sampleSize := 5
			if len(matchedDNSNames) < sampleSize {
				sampleSize = len(matchedDNSNames)
			}
			c.logger.Debug().
				Strs("sample_matched_dns", matchedDNSNames[:sampleSize]).
				Int("total_matched", len(matchedDNSNames)).
				Msg("Sample of machines matched by client-side filtering")
		}
		
		return filteredMachines, nil
	}
	
	c.logger.Info().
		Int("machine_count", len(machines)).
		Int("dns_names_count", len(dnsNames)).
		Time("since_time", sinceTime).
		Msg("Retrieved filtered machines from Citrix API")
	
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

	// Apply client-side DNS filtering if we have a valid machine list
	if len(c.validMachineDNS) > 0 {
		failureLogs = c.filterFailureLogsByMachineDNS(failureLogs, c.validMachineDNS)
	}

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

// getODataCollectionUnlimitedWithExpand retrieves ALL items with $expand support without pagination limits
func (c *citrixClient) getODataCollectionUnlimitedWithExpand(ctx context.Context, endpoint, filter, expand string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters - remove $top limit to get all items
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
	}

	c.logger.Info().
		Str("endpoint", endpoint).
		Str("filter", filter).
		Str("expand", expand).
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Msg("Completed unlimited OData collection retrieval with expand")

	// Convert to JSON and back to populate the result struct correctly
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal OData items: %v", err)
	}

	err = json.Unmarshal(jsonData, result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal OData items: %v", err)
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

		c.logger.Debug().
			Err(lastErr).
			Int("attempt", attempt+1).
			Int("max_attempts", c.config.MaxRetryAttempts).
			Msg("OData request failed, will retry if attempts remaining")
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

	// Apply client-side DNS filtering if we have a valid machine list
	if len(c.validMachineDNS) > 0 {
		connections = c.filterConnectionsByMachineDNS(connections, c.validMachineDNS)
	}

	return connections, nil
}

// SetValidMachineDNS sets the list of valid machine DNS names for filtering
func (c *citrixClient) SetValidMachineDNS(dnsNames []string) {
	c.validMachineDNS = dnsNames
	c.logger.Info().
		Int("dns_count", len(dnsNames)).
		Msg("Updated valid machine DNS list for client-side filtering")
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

// filterSessionsByMachineDNS filters sessions to only include those from specified machine DNS names
func (c *citrixClient) filterSessionsByMachineDNS(sessions []Session, validDNSNames []string) []Session {
	if len(validDNSNames) == 0 {
		// No filter, return all sessions
		return sessions
	}
	
	// Create a map for O(1) lookup with both DNS names and short names
	machineMap := make(map[string]bool)
	for _, dns := range validDNSNames {
		if dns != "" {
			// Add full DNS name
			machineMap[strings.ToLower(dns)] = true
			
			// Also add short name (first part before first dot)
			if dotIndex := strings.Index(dns, "."); dotIndex > 0 {
				shortName := dns[:dotIndex]
				machineMap[strings.ToLower(shortName)] = true
			}
		}
	}
	
	var filtered []Session
	var excludedCount int
	
	for _, session := range sessions {
		var machineName string
		var machineDNS string
		
		// Get machine name from expanded Machine data (preferred) or legacy MachineName field
		if session.Machine != nil {
			machineName = session.Machine.MachineName
			machineDNS = session.Machine.DnsName
		} else {
			machineName = session.MachineName
		}
		
		// Debug log the first few sessions to validate the fix
		if len(filtered) < 3 {
			c.logger.Info().
				Str("session_key", session.SessionKey).
				Str("expanded_machine_name", func() string { 
					if session.Machine != nil { return session.Machine.MachineName }
					return "null"
				}()).
				Str("expanded_machine_dns", func() string { 
					if session.Machine != nil { return session.Machine.DnsName }
					return "null"
				}()).
				Str("legacy_machine_name", session.MachineName).
				Str("session_user", session.UserName).
				Int("connection_state", session.ConnectionState).
				Int("valid_dns_count", len(validDNSNames)).
				Msg("🔍 Session structure debugging - WITH EXPAND")
		}
		
		// Try direct matching first (with machine name from expand)
		machineNameLower := strings.ToLower(machineName)
		if machineName == "" || machineMap[machineNameLower] {
			filtered = append(filtered, session)
			continue
		}
		
		// Try DNS name matching if available
		if machineDNS != "" && machineMap[strings.ToLower(machineDNS)] {
			filtered = append(filtered, session)
			c.logger.Debug().
				Str("session_key", session.SessionKey).
				Str("machine_dns", machineDNS).
				Msg("✅ Session included - matched via DNS name")
			continue
		}
		
		// Extract hostname from Domain\Hostname format and try matching
		hostnameOnly := machineName
		if backslashIndex := strings.LastIndex(machineName, "\\"); backslashIndex >= 0 {
			hostnameOnly = machineName[backslashIndex+1:]
		}
		
		if machineMap[strings.ToLower(hostnameOnly)] {
			filtered = append(filtered, session)
			c.logger.Debug().
				Str("session_key", session.SessionKey).
				Str("machine_name", machineName).
				Str("hostname_extracted", hostnameOnly).
				Msg("✅ Session included - matched via hostname extraction")
		} else {
			excludedCount++
			c.logger.Debug().
				Str("session_key", session.SessionKey).
				Str("machine_name", machineName).
				Str("machine_dns", machineDNS).
				Str("hostname_only", hostnameOnly).
				Str("user", session.UserName).
				Int("connection_state", session.ConnectionState).
				Msg("🚫 Session excluded - machine not in CVAD inventory")
		}
	}
	
	if excludedCount > 0 {
		c.logger.Info().
			Int("total_sessions", len(sessions)).
			Int("filtered_sessions", len(filtered)).
			Int("excluded_sessions", excludedCount).
			Msg("Applied client-side DNS filtering to sessions")
	}
	
	return filtered
}

// filterConnectionsByMachineDNS filters connections to only include those from specified machine DNS names
func (c *citrixClient) filterConnectionsByMachineDNS(connections []Connection, validDNSNames []string) []Connection {
	if len(validDNSNames) == 0 {
		// No filter, return all connections
		return connections
	}
	
	// Create a map for O(1) lookup
	dnsMap := make(map[string]bool)
	for _, dns := range validDNSNames {
		if dns != "" {
			dnsMap[strings.ToLower(dns)] = true
		}
	}
	
	var filtered []Connection
	var excludedCount int
	
	for _, conn := range connections {
		// Note: Connection doesn't have direct machine info, skip filtering for now
		// TODO: Need to join with Session via SessionKey to get machine info
		filtered = append(filtered, conn)
	}
	
	if excludedCount > 0 {
		c.logger.Info().
			Int("total_connections", len(connections)).
			Int("filtered_connections", len(filtered)).
			Int("excluded_connections", excludedCount).
			Msg("Applied client-side DNS filtering to connections")
	}
	
	return filtered
}

// filterFailureLogsByMachineDNS filters connection failure logs to only include those from specified machine DNS names
func (c *citrixClient) filterFailureLogsByMachineDNS(failures []ConnectionFailureLog, validDNSNames []string) []ConnectionFailureLog {
	if len(validDNSNames) == 0 {
		// No filter, return all failures
		return failures
	}
	
	// Create a map for O(1) lookup with both DNS names and short names
	machineMap := make(map[string]bool)
	for _, dns := range validDNSNames {
		if dns != "" {
			// Add full DNS name
			machineMap[strings.ToLower(dns)] = true
			
			// Also add short name (first part before first dot)
			if dotIndex := strings.Index(dns, "."); dotIndex > 0 {
				shortName := dns[:dotIndex]
				machineMap[strings.ToLower(shortName)] = true
			}
		}
	}
	
	var filtered []ConnectionFailureLog
	var excludedCount int
	
	for _, failure := range failures {
		// Check if failure's machine name is in our valid list
		// Note: ConnectionFailureLog has MachineName, not MachineDnsName
		machineName := strings.ToLower(failure.MachineName)
		if machineName == "" || machineMap[machineName] {
			filtered = append(filtered, failure)
		} else {
			excludedCount++
			c.logger.Debug().
				Int("failure_id", failure.Id).
				Str("machine_name", failure.MachineName).
				Str("user", failure.UserName).
				Msg("🚫 Connection failure excluded - machine not in CVAD inventory")
		}
	}
	
	if excludedCount > 0 {
		c.logger.Info().
			Int("total_failures", len(failures)).
			Int("filtered_failures", len(filtered)).
			Int("excluded_failures", excludedCount).
			Msg("Applied client-side DNS filtering to connection failures")
	}
	
	return filtered
}
