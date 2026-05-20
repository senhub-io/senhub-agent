// Package veeam provides monitoring capabilities for Veeam Backup & Replication v13 via REST API
package veeam

import "time"

// tokenResponse represents the OAuth2 token response from Veeam
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// paginatedResponse wraps paginated list responses from the Veeam API
type paginatedResponse[T any] struct {
	Data       []T        `json:"data"`
	Pagination pagination `json:"pagination"`
}

// pagination holds pagination metadata
type pagination struct {
	Total int `json:"total"`
	Count int `json:"count"`
	Skip  int `json:"skip"`
	Limit int `json:"limit"`
}

// serverInfo represents Veeam server information from /api/v1/serverInfo
type serverInfo struct {
	Name         string `json:"name"`
	BuildVersion string `json:"buildVersion"`
	Platform     string `json:"platform"`
}

// repository represents a Veeam backup repository state from /api/v1/backupInfrastructure/repositories/states
type repository struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	CapacityGB  float64 `json:"capacityGB"`
	FreeGB      float64 `json:"freeGB"`
	UsedSpaceGB float64 `json:"usedSpaceGB"`
}

// licenseInfo represents Veeam license details from /api/v1/license
type licenseInfo struct {
	Type                   string                 `json:"type"`
	Status                 string                 `json:"status"`
	ExpirationDate         time.Time              `json:"expirationDate"`
	InstanceLicenseSummary instanceLicenseSummary `json:"instanceLicenseSummary"`
	SocketLicenseSummary   socketLicenseSummary   `json:"socketLicenseSummary"`
}

// instanceLicenseSummary holds instance-based license counters
// Note: Veeam API returns these as floats (e.g. 85.0) not ints
type instanceLicenseSummary struct {
	LicensedInstancesNumber float64 `json:"licensedInstancesNumber"`
	UsedInstancesNumber     float64 `json:"usedInstancesNumber"`
}

// socketLicenseSummary holds socket-based license counters
type socketLicenseSummary struct {
	LicensedSocketsNumber float64 `json:"licensedSocketsNumber"`
	UsedSocketsNumber     float64 `json:"usedSocketsNumber"`
}

// proxy represents a Veeam backup proxy state from /api/v1/backupInfrastructure/proxies/states
type proxy struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	HostName   string `json:"hostName"`
	IsDisabled bool   `json:"isDisabled"`
	IsOnline   bool   `json:"isOnline"`
}

// jobState represents a consolidated job state from /api/v1/jobs/states
type jobState struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Status          string           `json:"status"`     // EJobStatus: Running, Inactive, Disabled, etc.
	LastResult      string           `json:"lastResult"` // ESessionResult: None, Success, Warning, Failed
	LastRun         *time.Time       `json:"lastRun"`
	NextRun         *time.Time       `json:"nextRun"`
	ObjectsCount    int              `json:"objectsCount"`
	ProgressPercent int              `json:"progressPercent"`
	RepositoryName  string           `json:"repositoryName"`
	SessionProgress *sessionProgress `json:"sessionProgress"`
}

// sessionProgress holds progress details for a running or completed session
type sessionProgress struct {
	Duration        string  `json:"duration"`
	ProcessingRate  *string `json:"processingRate"`
	Bottleneck      string  `json:"bottleneck"`      // ESessionBottleneckType
	ProcessedSize   *int64  `json:"processedSize"`   // bytes
	ReadSize        *int64  `json:"readSize"`        // bytes
	TransferredSize *int64  `json:"transferredSize"` // bytes
	ProgressPercent *int    `json:"progressPercent"`
}

// backupObject represents a protected backup object from /api/v1/backupObjects
type backupObject struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	PlatformName       string `json:"platformName"`
	RestorePointsCount int    `json:"restorePointsCount"`
	LastRunFailed      bool   `json:"lastRunFailed"`
}

// managedServer represents a managed server from /api/v1/backupInfrastructure/managedServers
type managedServer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"` // EManagedServersStatus: Available, Unavailable
}

// probeConfig holds parsed configuration for the Veeam probe
type probeConfig struct {
	Endpoint     string
	Username     string
	Password     string
	Interval     int
	Port         int
	VerifySSL    bool
	HoursToCheck int
}
