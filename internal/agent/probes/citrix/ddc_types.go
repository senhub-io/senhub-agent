package citrix

import (
	"encoding/json"
	"time"
)

// DDCMachine represents a machine from Delivery Controller API
type DDCMachine struct {
	Id                     string     `json:"Id"`
	Name                   string     `json:"Name"`
	DNSName                string     `json:"DnsName"`
	MachineName            string     `json:"MachineName"`
	SiteId                 string     `json:"SiteId,omitempty"`
	SiteName               string     `json:"SiteName,omitempty"`
	DeliveryGroupId        string     `json:"DeliveryGroupId,omitempty"`
	DeliveryGroupName      string     `json:"DeliveryGroupName,omitempty"`
	ControllerDNSName      string     `json:"AssociatedControllerDnsName,omitempty"`
	IsAssigned             bool       `json:"IsAssigned"`
	IsInMaintenanceMode    bool       `json:"InMaintenanceMode"`
	RegistrationState      string     `json:"RegistrationState"`
	LastConnectionTime     *time.Time `json:"LastConnectionTime,omitempty"`
	LastDeregistrationTime *time.Time `json:"LastDeregistrationTime,omitempty"`
	PowerState             string     `json:"PowerState,omitempty"`
	SessionCount           int        `json:"SessionCount"`
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

// DDCApplication represents an application from Delivery Controller API
type DDCApplication struct {
	Id                    string   `json:"Id"`
	Name                  string   `json:"Name"`
	PublishedName         string   `json:"PublishedName"`
	Description           string   `json:"Description,omitempty"`
	Enabled               bool     `json:"Enabled"`
	Visible               bool     `json:"Visible"`
	CommandLineExecutable string   `json:"CommandLineExecutable"`
	DeliveryGroupIds      []string `json:"AssociatedDeliveryGroupIds,omitempty"`
	SiteId                string   `json:"SiteId,omitempty"`
	SiteName              string   `json:"SiteName,omitempty"`
}

// DDCController represents a Delivery Controller
type DDCController struct {
	Id                 string     `json:"Id"`
	Name               string     `json:"Name"`
	DNSName            string     `json:"DnsName"`
	MachineName        string     `json:"MachineName"`
	SiteId             string     `json:"SiteId,omitempty"`
	SiteName           string     `json:"SiteName,omitempty"`
	ControllerState    string     `json:"State"`
	LastActivityTime   *time.Time `json:"LastActivityTime,omitempty"`
	DesktopsRegistered int        `json:"DesktopsRegistered"`
}

// DDCSession represents a session from Delivery Controller API
type DDCSession struct {
	Id                string     `json:"Id"`
	UserName          string     `json:"UserName"`
	UserUPN           string     `json:"UserUPN,omitempty"`
	MachineDNSName    string     `json:"MachineDnsName,omitempty"`
	MachineId         string     `json:"MachineId,omitempty"`
	DeliveryGroupId   string     `json:"DeliveryGroupId,omitempty"`
	DeliveryGroupName string     `json:"DeliveryGroupName,omitempty"`
	SessionState      string     `json:"SessionState"`
	StartTime         *time.Time `json:"StartTime,omitempty"`
	LogOnDuration     int        `json:"LogOnDuration,omitempty"` // in milliseconds
	ConnectionState   string     `json:"ConnectionState"`
	Protocol          string     `json:"Protocol,omitempty"`
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

// DDCApplicationsResponse represents the response from Applications endpoint
type DDCApplicationsResponse struct {
	Items             []DDCApplication `json:"Items"`
	ContinuationToken string           `json:"ContinuationToken,omitempty"`
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
