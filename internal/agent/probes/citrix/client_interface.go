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

	// GetSessionsByConnectionState retrieves sessions filtered by ConnectionState
	GetSessionsByConnectionState(ctx context.Context, connectionStates []int) ([]Session, error)

	// GetMachines retrieves machines data from the OData API
	GetMachines(ctx context.Context, sinceTime time.Time) ([]Machine, error)

	// GetMachinesFiltered retrieves machines filtered by DNS names from CVAD inventory
	GetMachinesFiltered(ctx context.Context, sinceTime time.Time, dnsNames []string) ([]Machine, error)

	// GetDesktopGroups retrieves desktop groups (delivery groups) data from the OData API
	GetDesktopGroups(ctx context.Context) ([]DesktopGroup, error)

	// GetConnectionFailureLogs retrieves connection failure logs from the OData API
	GetConnectionFailureLogs(ctx context.Context, sinceTime time.Time) ([]ConnectionFailureLog, error)

	// GetConnectionFailureLogsWithExpand retrieves connection failure logs with expanded data from the OData API
	GetConnectionFailureLogsWithExpand(ctx context.Context, sinceTime time.Time, expand []string) ([]ConnectionFailureLog, error)

	// GetConnectionFailureCategories retrieves connection failure category mappings from the OData API
	GetConnectionFailureCategories(ctx context.Context) ([]ConnectionFailureCategory, error)

	// GetDeliveryGroupById retrieves a specific delivery group by ID (fallback for missing groups)
	GetDeliveryGroupById(ctx context.Context, deliveryGroupId string) (*DesktopGroup, error)

	// GetConnections retrieves connection details with logon breakdown metrics
	GetConnections(ctx context.Context, sinceTime time.Time) ([]Connection, error)

	// GetLoadIndexes retrieves current load index data for machines
	GetLoadIndexes(ctx context.Context) ([]LoadIndex, error)

	// SetValidMachineDNS sets the list of valid machine DNS names for client-side filtering
	SetValidMachineDNS(dnsNames []string)
}

// ComponentConfig holds per-component connection config (Director, DDC, License Server)
type ComponentConfig struct {
	URL          string
	FallbackURLs []string
	VerifySSL    bool
	Auth         AuthConfig
}

// CitrixClientConfig holds the configuration for the Citrix client
type CitrixClientConfig struct {
	BaseURL            string
	FallbackURLs       []string
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
	SessionKey             string     `json:"SessionKey"`
	UserId                 int        `json:"UserId"`
	UserName               string     `json:"UserName"`
	DesktopGroupId         string     `json:"DesktopGroupId"`
	DesktopGroupName       string     `json:"DesktopGroupName"`
	MachineName            string     `json:"MachineName"`   // Legacy field, often empty
	MachineId              string     `json:"MachineId"`     // Machine GUID for linking
	SessionState           int        `json:"SessionState"`  // 2=disconnected, 3=connected, 5=active
	LogOnDuration          int        `json:"LogOnDuration"` // milliseconds
	StartTime              time.Time  `json:"StartTime"`
	EndTime                *time.Time `json:"EndTime,omitempty"`
	SessionStateChangeTime time.Time  `json:"SessionStateChangeTime"`
	ModifiedDate           time.Time  `json:"ModifiedDate"`
	ConnectionState        int        `json:"ConnectionState"`
	Protocol               string     `json:"Protocol"`
	ClientName             string     `json:"ClientName"`
	ClientIPAddress        string     `json:"ClientIPAddress"`
	SessionType            int        `json:"SessionType"`
	Hidden                 bool       `json:"Hidden"`            // Hidden sessions are zombie sessions
	Machine                *Machine   `json:"Machine,omitempty"` // Expanded machine data
}

// Machine represents a Citrix machine from the OData API
type Machine struct {
	MachineId          string     `json:"Id"`                       // Machine ID
	MachineName        string     `json:"Name"`                     // Machine name (can be null)
	DnsName            string     `json:"DnsName"`                  // DNS name (can be null)
	DesktopGroupId     string     `json:"DesktopGroupId"`           // Desktop group ID
	DesktopGroupName   string     `json:"DesktopGroupName"`         // Desktop group name
	RegistrationState  int        `json:"CurrentRegistrationState"` // 0=unregistered, 1=registered, 2=agent_error
	FaultState         int        `json:"FaultState"`               // 1=healthy/none, others=failed
	PowerState         int        `json:"CurrentPowerState"`        // Power state as int
	MachineRole        int        `json:"MachineRole"`              // 0=unknown, 1=single_session, 2=multi_session
	ControllerDNSName  string     `json:"ControllerDNSName"`
	ModifiedDate       time.Time  `json:"ModifiedDate"`
	LastConnectionTime *time.Time `json:"LastConnectionTime,omitempty"`
	LastConnectionUser string     `json:"LastConnectionUser"`
	SessionCount       int        `json:"CurrentSessionCount"` // Current session count
	LoadIndex          int        `json:"LoadIndex"`
	InMaintenanceMode  bool       `json:"IsInMaintenanceMode"` // Maintenance mode flag
	OSType             string     `json:"OSType"`              // OS type (can be null)
	LifecycleState     int        `json:"LifecycleState"`      // Lifecycle state
}

// DesktopGroup represents a Citrix delivery group from the OData API
type DesktopGroup struct {
	DesktopGroupId string `json:"DesktopGroupId"` // Real Citrix server
	Id             string `json:"Id"`             // Mock server and some Citrix versions
	Name           string `json:"Name"`
	Description    string `json:"Description"`
	Enabled        bool   `json:"Enabled"`
	SessionSharing bool   `json:"SessionSharing"`
	DeliveryType   int    `json:"DeliveryType"`
	TotalMachines  int    `json:"TotalMachines"`
	MachinesInUse  int    `json:"MachinesInUse"`
	PublishedName  string `json:"PublishedName"`
}

// GetEffectiveId returns the effective ID, preferring DesktopGroupId over Id
func (dg *DesktopGroup) GetEffectiveId() string {
	if dg.DesktopGroupId != "" {
		return dg.DesktopGroupId
	}
	return dg.Id
}

// ConnectionFailureLog represents a connection failure log entry from the OData API
type ConnectionFailureLog struct {
	Id                         int       `json:"Id"`
	FailureDate                time.Time `json:"FailureDate"`
	ConnectionFailureEnumValue int       `json:"ConnectionFailureEnumValue"` // The actual failure code from Citrix
	DesktopGroupId             string    `json:"DesktopGroupId"`
	DesktopGroupName           string    `json:"DesktopGroupName"`
	UserName                   string    `json:"UserName"`
	MachineName                string    `json:"MachineName"`
	MachineId                  string    `json:"MachineId"` // Machine GUID for black hole detection
	FailureDetails             string    `json:"FailureDetails"`
	ErrorCode                  string    `json:"ErrorCode"`
	ClientName                 string    `json:"ClientName"`
	ClientIPAddress            string    `json:"ClientIPAddress"`
}

// ConnectionFailureCategory represents the mapping between failure codes and categories
type ConnectionFailureCategory struct {
	Id                         int       `json:"Id"`
	ConnectionFailureEnumValue int       `json:"ConnectionFailureEnumValue"`
	Category                   int       `json:"Category"`
	CreatedDate                time.Time `json:"CreatedDate"`
	ModifiedDate               time.Time `json:"ModifiedDate"`
}

// Connection represents a connection with detailed logon metrics from the OData API
type Connection struct {
	Id                     int        `json:"Id"`
	ClientName             string     `json:"ClientName"`
	ClientAddress          string     `json:"ClientAddress"`
	Protocol               string     `json:"Protocol"`
	IsReconnect            bool       `json:"IsReconnect"`
	LogOnStartDate         time.Time  `json:"LogOnStartDate"`
	LogOnEndDate           time.Time  `json:"LogOnEndDate"`
	BrokeringDuration      int        `json:"BrokeringDuration"` // milliseconds
	BrokeringDate          time.Time  `json:"BrokeringDate"`
	VMStartStartDate       *time.Time `json:"VMStartStartDate"`
	VMStartEndDate         *time.Time `json:"VMStartEndDate"`
	HdxStartDate           time.Time  `json:"HdxStartDate"`
	HdxEndDate             time.Time  `json:"HdxEndDate"`
	AuthenticationDuration int        `json:"AuthenticationDuration"` // milliseconds
	GpoStartDate           time.Time  `json:"GpoStartDate"`
	GpoEndDate             time.Time  `json:"GpoEndDate"`
	LogOnScriptsStartDate  time.Time  `json:"LogOnScriptsStartDate"`
	LogOnScriptsEndDate    time.Time  `json:"LogOnScriptsEndDate"`
	ProfileLoadStartDate   time.Time  `json:"ProfileLoadStartDate"`
	ProfileLoadEndDate     time.Time  `json:"ProfileLoadEndDate"`
	InteractiveStartDate   time.Time  `json:"InteractiveStartDate"`
	InteractiveEndDate     time.Time  `json:"InteractiveEndDate"`
	SessionKey             string     `json:"SessionKey"`
	CreatedDate            time.Time  `json:"CreatedDate"`
	ModifiedDate           time.Time  `json:"ModifiedDate"`
}

// LoadIndex represents load index data for a machine from the OData API
// The load index is reported by the VDA and indicates resource utilization.
// EffectiveLoadIndex ranges from 0 (idle) to 10000 (full load).
type LoadIndex struct {
	Id                 int       `json:"Id"`
	MachineId          string    `json:"MachineId"`
	Cpu                int       `json:"Cpu"`                // CPU load component (0-10000)
	Memory             int       `json:"Memory"`             // Memory load component (0-10000)
	Disk               int       `json:"Disk"`               // Disk load component (0-10000)
	Network            int       `json:"Network"`            // Network load component (0-10000)
	SessionCount       int       `json:"SessionCount"`       // Session count load component (0-10000)
	EffectiveLoadIndex int       `json:"EffectiveLoadIndex"` // Combined effective load (0-10000)
	ModifiedDate       time.Time `json:"ModifiedDate"`
}

// LoadIndex threshold constants
const (
	LoadIndexFull       = 10000 // Machine at full capacity
	LoadIndexOverloaded = 8000  // Threshold for "overloaded" (80%)
	LoadIndexHigh       = 6000  // Threshold for "high load" (60%)
)

// Controller represents a Citrix controller from the API
type Controller struct {
	ControllerDNSName     string            `json:"ControllerDNSName"`
	State                 string            `json:"State"`                 // Online, Offline
	SiteDatabaseStatus    string            `json:"SiteDatabaseStatus"`    // Connected, Disconnected
	LicenseServerStatus   string            `json:"LicenseServerStatus"`   // Connected, Disconnected
	ConfigLoggingDBStatus string            `json:"ConfigLoggingDBStatus"` // Connected, Disconnected
	MonitoringDBStatus    string            `json:"MonitoringDBStatus"`    // Connected, Disconnected
	ServicesRunning       map[string]string `json:"ServicesRunning"`
	LastContactTime       time.Time         `json:"LastContactTime"`
	MachinesRegistered    int               `json:"MachinesRegistered"`
}

// ODataResponse represents the standard OData response wrapper
type ODataResponse struct {
	Context  string        `json:"@odata.context"`
	Count    *int          `json:"@odata.count,omitempty"`
	NextLink string        `json:"@odata.nextLink,omitempty"`
	Value    []interface{} `json:"value"`
}

// AuthenticationMethod represents supported authentication methods
type AuthenticationMethod string

const (
	AuthMethodNTLM     AuthenticationMethod = "ntlm"
	AuthMethodBasic    AuthenticationMethod = "basic"
	AuthMethodKerberos AuthenticationMethod = "kerberos"
)

// Failure category constants (based on Citrix ConnectionFailureCategories)
const (
	FailureCategoryClientConnection = 0 // Client-side connection issues
	FailureCategoryConfiguration    = 1 // Configuration/setup errors
	FailureCategoryMachine          = 2 // Machine/VDA failures
	FailureCategoryCapacity         = 3 // Capacity/resource issues
	FailureCategoryLicense          = 4 // Licensing issues
	FailureCategoryOther            = 5 // Other/Unknown issues
)

// Session state constants (based on Citrix ConnectionState enum)
const (
	SessionStateUnknown      = 0 // Unknown state
	SessionStateConnected    = 1 // Connected - actively connected to desktop
	SessionStateDisconnected = 2 // Disconnected - but session exists
	SessionStateTerminated   = 3 // Terminated
	SessionStatePreparing    = 4 // PreparingSession
	SessionStateActive       = 5 // Active
	SessionStateReconnecting = 6 // Reconnecting
	SessionStateOther        = 8 // Other
	SessionStatePending      = 9 // Pending
)

// Registration state constants for machines
const (
	RegistrationStateUnregistered = 0
	RegistrationStateRegistered   = 1
	RegistrationStateAgentError   = 2
)

// Fault state constants for machines (based on Citrix MachineFaultStateCode enum)
const (
	FaultStateUnknown       = 0 // Unknown
	FaultStateNone          = 1 // None - healthy machine
	FaultStateFailedToStart = 2 // Last power-on operation failed
	FaultStateStuckOnBoot   = 3 // Machine might not have booted
	FaultStateUnregistered  = 4 // Unregistered
	FaultStateMaxCapacity   = 5 // Max capacity
	FaultStateVMNotFound    = 6 // Virtual machine not found
)

// Delivery type constants for desktop groups
const (
	DeliveryTypeDesktopsOnly    = 0
	DeliveryTypeAppsOnly        = 1
	DeliveryTypeDesktopsAndApps = 2
)

// Session type constants
const (
	SessionTypeApplication = 0
	SessionTypeDesktop     = 1
)
