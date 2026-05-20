package citrix

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CVADTime handles the custom date format used by CVAD API
type CVADTime struct {
	time.Time
}

// UnmarshalJSON parses CVAD date format: "09/10/2025 13:34:55"
func (ct *CVADTime) UnmarshalJSON(data []byte) error {
	// Remove quotes
	dateStr := strings.Trim(string(data), `"`)
	if dateStr == "null" || dateStr == "" {
		return nil
	}

	// Try CVAD format: "09/10/2025 13:34:55"
	if parsedTime, err := time.Parse("01/02/2006 15:04:05", dateStr); err == nil {
		ct.Time = parsedTime
		return nil
	}

	// Fallback to standard ISO format
	if parsedTime, err := time.Parse(time.RFC3339, dateStr); err == nil {
		ct.Time = parsedTime
		return nil
	}

	return fmt.Errorf("unable to parse CVAD date format: %s", dateStr)
}

// MarshalJSON outputs in standard format
func (ct CVADTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ct.Time.Format(time.RFC3339))
}

// DDCMachine represents a machine from Delivery Controller API
type DDCMachine struct {
	Id                     string    `json:"Id"`
	Name                   string    `json:"Name"`
	DNSName                string    `json:"DnsName"`
	MachineName            string    `json:"MachineName"`
	SiteId                 string    `json:"SiteId,omitempty"`
	SiteName               string    `json:"SiteName,omitempty"`
	DeliveryGroupId        string    `json:"DeliveryGroupId,omitempty"`
	DeliveryGroupName      string    `json:"DeliveryGroupName,omitempty"`
	ControllerDNSName      string    `json:"AssociatedControllerDnsName,omitempty"`
	IsAssigned             bool      `json:"IsAssigned"`
	IsInMaintenanceMode    bool      `json:"InMaintenanceMode"`
	RegistrationState      string    `json:"RegistrationState"`
	LastConnectionTime     *CVADTime `json:"LastConnectionTime,omitempty"`
	LastDeregistrationTime *CVADTime `json:"LastDeregistrationTime,omitempty"`
	PowerState             string    `json:"PowerState,omitempty"`
	SessionCount           int       `json:"SessionCount"`
}

// DDCDeliveryGroup represents a delivery group from Delivery Controller API
type DDCDeliveryGroup struct {
	Id                    string   `json:"Id"`
	Name                  string   `json:"Name"`
	Description           string   `json:"Description,omitempty"`
	SiteId                string   `json:"SiteId,omitempty"`
	SiteName              string   `json:"SiteName,omitempty"`
	Enabled               bool     `json:"Enabled"`
	InMaintenanceMode     bool     `json:"InMaintenanceMode"`
	TotalMachines         int      `json:"TotalMachines"`
	TotalAssignedMachines int      `json:"AssignedMachineCount"`
	SessionSupport        string   `json:"SessionSupport"` // SingleSession, MultiSession
	DeliveryType          string   `json:"DeliveryKind"`   // DesktopsOnly, AppsOnly, DesktopsAndApps
	MachineIds            []string `json:"AssociatedMachineIds,omitempty"`
}

// DDCController represents a Delivery Controller
type DDCController struct {
	Id                 string    `json:"Id"`
	Name               string    `json:"Name"`
	DNSName            string    `json:"DnsName"`
	MachineName        string    `json:"MachineName"`
	SiteId             string    `json:"SiteId,omitempty"`
	SiteName           string    `json:"SiteName,omitempty"`
	ControllerState    string    `json:"State"`
	LastActivityTime   *CVADTime `json:"LastActivityTime,omitempty"`
	DesktopsRegistered int       `json:"DesktopsRegistered"`
}

// DDCSession represents a session from Delivery Controller API
type DDCSession struct {
	Id                string    `json:"Id"`
	UserName          string    `json:"UserName"`
	UserUPN           string    `json:"UserUPN,omitempty"`
	MachineDNSName    string    `json:"MachineDnsName,omitempty"`
	MachineId         string    `json:"MachineId,omitempty"`
	DeliveryGroupId   string    `json:"DeliveryGroupId,omitempty"`
	DeliveryGroupName string    `json:"DeliveryGroupName,omitempty"`
	SessionState      string    `json:"SessionState"`
	StartTime         *CVADTime `json:"StartTime,omitempty"`
	LogOnDuration     int       `json:"LogOnDuration,omitempty"` // in milliseconds
	ConnectionState   string    `json:"ConnectionState"`
	Protocol          string    `json:"Protocol,omitempty"`
}

// DDCPaginatedResponse represents a paginated API response
type DDCPaginatedResponse struct {
	Items             json.RawMessage `json:"Items"`
	ContinuationToken string          `json:"ContinuationToken,omitempty"`
	TotalItems        int             `json:"TotalItems,omitempty"`
}

// DDCSiteDetails represents detailed site information
type DDCSiteDetails struct {
	Site
	TotalMachines      int      `json:"TotalMachines"`
	ActiveSessions     int      `json:"ActiveSessions"`
	DeliveryGroups     []string `json:"DeliveryGroups"`
	Controllers        []string `json:"Controllers"`
	RegisteredMachines int      `json:"RegisteredMachines"`
}

// DDCSiteLicenseInfo represents licensing information from the CVAD Sites endpoint
type DDCSiteLicenseInfo struct {
	LicenseServerName             string `json:"LicenseServerName"`
	LicenseServerPort             int    `json:"LicenseServerPort"`
	LicenseServerUri              string `json:"LicenseServerUri"`
	LicensingModel                string `json:"LicensingModel"`                // "UserDevice", "Concurrent"
	ProductEdition                string `json:"ProductEdition"`                // "Premium", "Advanced", "Standard"
	LicensedSessionsActive        int    `json:"LicensedSessionsActive"`        // Currently active licensed sessions
	PeakConcurrentLicenseUsers    int    `json:"PeakConcurrentLicenseUsers"`    // Peak concurrent users
	TotalUniqueLicenseUsers       int    `json:"TotalUniqueLicenseUsers"`       // Total unique users
	LicenseGraceSessionsRemaining int    `json:"LicenseGraceSessionsRemaining"` // Grace sessions left
	LicensingGracePeriodActive    bool   `json:"LicensingGracePeriodActive"`    // Grace period active
	LicensingGraceHoursLeft       int    `json:"LicensingGraceHoursLeft"`       // Grace hours remaining
}

// DDCMachinesResponse represents the response from Machines endpoint
type DDCMachinesResponse struct {
	Items             []DDCMachine `json:"Items"`
	ContinuationToken string       `json:"ContinuationToken,omitempty"`
}

// DDCDeliveryGroupsResponse represents the response from DeliveryGroups endpoint
type DDCDeliveryGroupsResponse struct {
	Items             []DDCDeliveryGroup `json:"Items"`
	ContinuationToken string             `json:"ContinuationToken,omitempty"`
}

// DDCControllersResponse represents the response from Controllers endpoint
type DDCControllersResponse struct {
	Items             []DDCController `json:"Items"`
	ContinuationToken string          `json:"ContinuationToken,omitempty"`
}

// DDCSessionsResponse represents the response from Sessions endpoint
type DDCSessionsResponse struct {
	Items             []DDCSession `json:"Items"`
	ContinuationToken string       `json:"ContinuationToken,omitempty"`
}

// DDCMeResponse represents the /me endpoint response with current user info
type DDCMeResponse struct {
	UserId                string        `json:"UserId"`
	DisplayName           string        `json:"DisplayName"`
	ExpiryTime            string        `json:"ExpiryTime"`
	RefreshExpirationTime string        `json:"RefreshExpirationTime"`
	VerifiedEmail         interface{}   `json:"VerifiedEmail"`
	IsCspCustomer         bool          `json:"IsCspCustomer"`
	Customers             []DDCCustomer `json:"Customers"`
}

// DDCCustomer represents a customer in the /me response
type DDCCustomer struct {
	Id    string    `json:"Id"`
	Name  *string   `json:"Name"`
	Sites []DDCSite `json:"Sites"`
}

// DDCSite represents a site in the /me response
type DDCSite struct {
	Id   string `json:"Id"`
	Name string `json:"Name"`
}
