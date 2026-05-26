package configuration

// Shared configuration data types — extracted from the
// (now-deleted) remoteConfiguration.go so both LocalConfiguration and
// the test mocks can keep using them without re-importing the SaaS
// loader. The type names retain the historical `Remote*` prefix
// because they're referenced through that name from many sites
// (data_store, auto_update, sensor tests, …); a global rename is
// deliberately deferred to a follow-up to keep this PR scoped.

// StorageConfigParams holds the free-form parameter map associated
// with a single strategy entry (HTTP bind address, OTLP endpoint, …).
type StorageConfigParams = map[string]interface{}

// StorageConfig is one strategy entry from the storage/strategies list.
type StorageConfig struct {
	Name   string              `json:"name"`
	Params StorageConfigParams `json:"params"`
}

// ProbeConfigParams holds the free-form parameter map for a single
// probe entry (host, credentials, intervals, …).
type ProbeConfigParams = map[string]interface{}

// ProbeConfig is one probe entry from the probes list.
type ProbeConfig struct {
	Name   string            `json:"name"`
	Type   string            `json:"type,omitempty"`
	Params ProbeConfigParams `json:"params"`
}

// AgentConfig is the small "identity" block (key, license, version,
// update settings). The pre-0.2.0 RemoteConfiguration also stored a
// RegistryUrl here for SaaS-side update polling; that field is kept
// to avoid a JSON-schema break for callers that still serialize it.
type AgentConfig struct {
	RegistryUrl         string `json:"registry_url"`
	Version             string `json:"version"`
	UpdateCheckInterval any    `json:"update_check_interval" default:"3600"`
	License             string `json:"license,omitempty"`
	AuthenticationKey   string `json:"authentication_key,omitempty"`
}

// RemoteConfigurationData is the full configuration shape consumed by
// the agent's data store and sensor pool. The "Remote" prefix is
// historical; in 0.2.0+ the only producer is LocalConfiguration.
//
// TODO(#138): rename RemoteConfigurationData / RemoteConfiguration*
// away from the misleading "Remote" prefix once every consumer
// (data_store, sensor, auto_update, http strategy) is migrated.
// Deferred from the v0.2.0 PR to keep that diff scoped.
type RemoteConfigurationData struct {
	StorageConfig []StorageConfig `json:"storage"`
	Probes        []ProbeConfig   `json:"probes"`
	Agent         AgentConfig     `json:"agent"`
	Cache         *CacheConfig    `json:"cache,omitempty"`
}
