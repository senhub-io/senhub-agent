package citrix

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
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
	filters    *ClientFilters // DNS filtering functionality extracted to separate module
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
		filters:    NewClientFilters(baseLogger),
	}

	return client, nil
}

// Connect establishes a connection to the Citrix OData API endpoint
func (c *citrixClient) Connect(ctx context.Context) error {
	c.logger.Debug().
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

	c.logger.Debug().
		Int("status_code", resp.StatusCode).
		Msg("Successfully connected to Citrix OData API")

	return nil
}

// Disconnect closes the connection
func (c *citrixClient) Disconnect(ctx context.Context) error {
	c.logger.Debug().Msg("Disconnecting from Citrix OData API")
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
	if len(c.filters.GetValidMachineDNS()) > 0 {
		sessions = c.filters.FilterSessionsByMachineDNS(sessions)
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
	if len(c.filters.GetValidMachineDNS()) > 0 {
		sessions = c.filters.FilterSessionsByMachineDNS(sessions)
	}

	return sessions, nil
}

// GetMachines retrieves machines data from the OData API.
// By default, filters to machines assigned to a Delivery Group (DesktopGroupId ne null)
// to match Director Console behavior and exclude orphan/test/infrastructure machines.
func (c *citrixClient) GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error) {
	endpoint := "/Machines"

	// Base filter: only machines in a Delivery Group (matches Director Console view)
	// This excludes orphan machines, test machines, and infrastructure servers
	// that exist in the SQL database but are not part of any active Delivery Group.
	baseFilter := "DesktopGroupId ne null"

	var filter string
	if !sinceTime.IsZero() {
		filter = fmt.Sprintf("%s and ModifiedDate ge %s", baseFilter, formatODataDateTime(sinceTime))
		c.logger.Debug().
			Time("since_time", sinceTime).
			Str("filter", filter).
			Msg("Getting machines with time and Delivery Group filter")
	} else {
		filter = baseFilter
		c.logger.Debug().
			Str("filter", filter).
			Msg("Getting machines with Delivery Group filter")
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
			c.logger.Debug().
				Strs("cvad_sample_dns", validDnsNames[:sampleSize]).
				Int("total_cvad_dns", len(validDnsNames)).
				Msg("📋 Sample of CVAD inventory DNS names used for filtering")
		}

		c.logger.Debug().
			Int("cvad_dns_names", len(dnsNames)).
			Int("odata_machines", len(allMachines)).
			Msg("Starting client-side DNS filtering - detailed analysis")

		var filteredMachines []Machine
		var matchedDNSNames []string
		var notMatchedCount int
		processedDNS := make(map[string]bool)     // Track processed DNS to detect duplicates in OData
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

					c.logger.Debug().
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
						c.logger.Debug().
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
				c.logger.Debug().
					Str("machine_dns", machine.DnsName).
					Str("machine_name", machine.MachineName).
					Str("machine_id", machine.MachineId).
					Int("registration_state", machine.RegistrationState).
					Int("fault_state", machine.FaultState).
					Msg("🚫 OData machine NOT in CVAD inventory filter")
			}
		}

		c.logger.Debug().
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

	c.logger.Debug().
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

	c.logger.Debug().
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
	if len(c.filters.GetValidMachineDNS()) > 0 {
		failureLogs = c.filters.FilterFailureLogsByMachineDNS(failureLogs)
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
	if len(c.filters.GetValidMachineDNS()) > 0 {
		connections = c.filters.FilterConnectionsByMachineDNS(connections)
	}

	return connections, nil
}

// SetValidMachineDNS sets the list of valid machine DNS names for filtering
func (c *citrixClient) SetValidMachineDNS(dnsNames []string) {
	c.filters.SetValidMachineDNS(dnsNames)
}

// HTTP and OData helper methods moved to citrix_client_http.go for better code organization
