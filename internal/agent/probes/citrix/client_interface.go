// Package citrix provides monitoring capabilities for Citrix Virtual Apps and Desktops via OData API
package citrix

import (
	"context"
	"time"
)

// CitrixClient defines the interface for interacting with Citrix Director/Monitor OData API
type CitrixClient interface {
	// Connect establishes a connection to the Citrix OData API endpoint
	Connect(ctx context.Context) error

	// Disconnect closes the connection
	Disconnect(ctx context.Context) error

	// GetSessions retrieves sessions data from the OData API
	GetSessions(ctx context.Context, sinceTime time.Time) ([]Session, error)

	// GetMachines retrieves machines data from the OData API
	GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error)

	// GetDesktopGroups retrieves desktop groups (delivery groups) data from the OData API
	GetDesktopGroups(ctx context.Context) ([]DesktopGroup, error)

	// GetConnectionFailureLogs retrieves connection failure logs from the OData API
	GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error)

	// GetControllerStatus retrieves controller status information
	GetControllerStatus(ctx context.Context) ([]Controller, error)
}

// CitrixClientConfig holds the configuration for the Citrix client
type CitrixClientConfig struct {
	BaseURL            string
	Environment        string
	AuthMethod         string
	Username           string
	Password           string
	VerifySSL          bool
	Timeout            time.Duration
	MaxRetryAttempts   int
	RetryBackoffFactor float64
}

// Session represents a Citrix session from the OData API
type Session struct {
	SessionKey          string    `json:"SessionKey"`
	UserId              string    `json:"UserId"`
	UserName            string    `json:"UserName"`
	DesktopGroupId      string    `json:"DesktopGroupId"`
	DesktopGroupName    string    `json:"DesktopGroupName"`
	MachineName         string    `json:"MachineName"`
	SessionState        int       `json:"SessionState"`        // 2=disconnected, 3=connected, 5=active
	LogOnDuration       int       `json:"LogOnDuration"`       // milliseconds
	StartTime           time.Time `json:"StartTime"`
	EndTime             *time.Time `json:"EndTime,omitempty"`
	SessionStateChangeTime time.Time `json:"SessionStateChangeTime"`
	ModifiedDate        time.Time `json:"ModifiedDate"`
	ConnectionState     int       `json:"ConnectionState"`
	Protocol            string    `json:"Protocol"`
	ClientName          string    `json:"ClientName"`
	ClientIPAddress     string    `json:"ClientIPAddress"`
	SessionType         string    `json:"SessionType"`
}

// Machine represents a Citrix machine from the OData API
type Machine struct {
	MachineId           string    `json:"MachineId"`
	MachineName         string    `json:"MachineName"`
	DnsName             string    `json:"DnsName"`
	DesktopGroupId      string    `json:"DesktopGroupId"`
	DesktopGroupName    string    `json:"DesktopGroupName"`
	RegistrationState   int       `json:"RegistrationState"`   // 0=unregistered, 1=registered, 2=agent_error
	FaultState          int       `json:"FaultState"`          // 0=healthy, others=failed
	PowerState          string    `json:"PowerState"`
	SessionSupport      string    `json:"SessionSupport"`      // MultiSession, SingleSession
	ControllerDNSName   string    `json:"ControllerDNSName"`
	ModifiedDate        time.Time `json:"ModifiedDate"`
	LastConnectionTime  *time.Time `json:"LastConnectionTime,omitempty"`
	LastConnectionUser  string    `json:"LastConnectionUser"`
	SessionCount        int       `json:"SessionCount"`
	LoadIndex           int       `json:"LoadIndex"`
	InMaintenanceMode   bool      `json:"InMaintenanceMode"`
}

// DesktopGroup represents a Citrix delivery group from the OData API
type DesktopGroup struct {
	DesktopGroupId   string `json:"DesktopGroupId"`
	Name             string `json:"Name"`
	Description      string `json:"Description"`
	Enabled          bool   `json:"Enabled"`
	SessionSharing   bool   `json:"SessionSharing"`
	DeliveryType     string `json:"DeliveryType"`
	TotalMachines    int    `json:"TotalMachines"`
	MachinesInUse    int    `json:"MachinesInUse"`
	PublishedName    string `json:"PublishedName"`
}

// ConnectionFailureLog represents a connection failure log entry from the OData API
type ConnectionFailureLog struct {
	Id              string    `json:"Id"`
	FailureDate     time.Time `json:"FailureDate"`
	FailureType     int       `json:"FailureType"`     // 1=client_connection_failures, 2=configuration_errors, etc.
	DesktopGroupId  string    `json:"DesktopGroupId"`
	DesktopGroupName string   `json:"DesktopGroupName"`
	UserName        string    `json:"UserName"`
	MachineName     string    `json:"MachineName"`
	FailureDetails  string    `json:"FailureDetails"`
	ErrorCode       string    `json:"ErrorCode"`
	ClientName      string    `json:"ClientName"`
	ClientIPAddress string    `json:"ClientIPAddress"`
}

// Controller represents a Citrix controller from the API
type Controller struct {
	ControllerDNSName       string             `json:"ControllerDNSName"`
	State                   string             `json:"State"`                   // Online, Offline
	SiteDatabaseStatus      string             `json:"SiteDatabaseStatus"`      // Connected, Disconnected
	LicenseServerStatus     string             `json:"LicenseServerStatus"`     // Connected, Disconnected
	ConfigLoggingDBStatus   string             `json:"ConfigLoggingDBStatus"`   // Connected, Disconnected
	MonitoringDBStatus      string             `json:"MonitoringDBStatus"`      // Connected, Disconnected
	ServicesRunning         map[string]string  `json:"ServicesRunning"`
	LastContactTime         time.Time          `json:"LastContactTime"`
	MachinesRegistered      int                `json:"MachinesRegistered"`
}

// ODataResponse represents the standard OData response wrapper
type ODataResponse struct {
	Context      string        `json:"@odata.context"`
	Count        *int          `json:"@odata.count,omitempty"`
	NextLink     string        `json:"@odata.nextLink,omitempty"`
	Value        []interface{} `json:"value"`
}

// AuthenticationMethod represents supported authentication methods
type AuthenticationMethod string

const (
	AuthMethodNTLM     AuthenticationMethod = "ntlm"
	AuthMethodBasic    AuthenticationMethod = "basic"
	AuthMethodKerberos AuthenticationMethod = "kerberos"
)

// Failure type constants for connection failures
const (
	FailureTypeClientConnection  = 1
	FailureTypeConfiguration     = 2
	FailureTypeMachine          = 3
	FailureTypeCapacity         = 4
	FailureTypeLicense          = 5
)

// Session state constants
const (
	SessionStateDisconnected = 2
	SessionStateConnected    = 3
	SessionStateActive       = 5
)

// Registration state constants for machines
const (
	RegistrationStateUnregistered = 0
	RegistrationStateRegistered   = 1
	RegistrationStateAgentError   = 2
)

// Fault state constants for machines
const (
	FaultStateHealthy = 0
	FaultStateFailed  = 1
)