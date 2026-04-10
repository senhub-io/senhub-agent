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

// job represents a Veeam backup job from /api/v1/jobs
type job struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IsDisabled bool   `json:"isDisabled"`
}

// session represents a Veeam job session from /api/v1/sessions
type session struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Result       string    `json:"result"`
	State        string    `json:"state"`
	CreationTime time.Time `json:"creationTime"`
	EndTime      time.Time `json:"endTime"`
}

// repository represents a Veeam backup repository from /api/v1/backupInfrastructure/repositories
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
	Type                  string                `json:"type"`
	Status                string                `json:"status"`
	ExpirationDate        time.Time             `json:"expirationDate"`
	InstanceLicenseSummary instanceLicenseSummary `json:"instanceLicenseSummary"`
	SocketLicenseSummary  socketLicenseSummary   `json:"socketLicenseSummary"`
}

// instanceLicenseSummary holds instance-based license counters
type instanceLicenseSummary struct {
	LicensedInstancesNumber int `json:"licensedInstancesNumber"`
	UsedInstancesNumber     int `json:"usedInstancesNumber"`
}

// socketLicenseSummary holds socket-based license counters
type socketLicenseSummary struct {
	LicensedSocketsNumber int `json:"licensedSocketsNumber"`
	UsedSocketsNumber     int `json:"usedSocketsNumber"`
}

// proxy represents a Veeam backup proxy from /api/v1/backupInfrastructure/proxies
type proxy struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Server       proxyServer `json:"server"`
	IsDisabled   bool        `json:"isDisabled"`
	MaxTaskCount int         `json:"maxTaskCount"`
}

// proxyServer holds proxy server details
type proxyServer struct {
	Name string `json:"name"`
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
