package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"

	"senhub-agent.go/internal/agent/services/governance"
)

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
	// CustomTags are applied to every datapoint this probe instance emits;
	// they override agent global_tags (and built-in probe tags) on a key
	// conflict. Matched to datapoints by probe name in the data store.
	CustomTags map[string]string `json:"custom_tags,omitempty" yaml:"custom_tags,omitempty"`
	// Governance is the operator-asserted ownership, criticality, location,
	// lifecycle and labels of what THIS instance observes: the database, the
	// device, the remote application. It is stamped on the entities the
	// instance emits and on its metrics and logs, so two instances on one
	// host can belong to two different application chains. The agent-level
	// governance block describes the host itself and is not inherited here.
	// Kept raw: the governance package owns the shape and the closed sets.
	Governance map[string]interface{} `json:"governance,omitempty" yaml:"governance,omitempty"`
	// Enabled turns a probe off without deleting its configuration.
	//
	// A POINTER on purpose: absent must stay distinguishable from an explicit
	// `enabled: false`. With a plain bool the zero value is false, so every
	// existing configuration — none of which carries the field — would go dark
	// on the upgrade that introduced it.
	//
	// Before this existed the only switch was presence in the file, so muting a
	// probe meant deleting its entry and with it the credentials, intervals and
	// custom tags that took effort to get right.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// LogStrategies routes this probe's LOG records to specific outputs,
	// the way the metric router already routes its datapoints.
	//
	// It is a separate field, and deliberately not the probe's metric
	// target list, because the two answer different questions. The
	// syslog probe sends its METRICS to the legacy event output; reusing
	// that list for its logs would cut them off the OTLP rail entirely
	// (#836). Only outputs that can consume logs are accepted — naming
	// one that cannot would silently mute the probe.
	//
	// Absent (the default) means every log output receives the records,
	// which is what every existing configuration does today.
	LogStrategies []string `json:"log_strategies,omitempty" yaml:"log_strategies,omitempty"`
}

// LogCapableStrategies is the set of outputs that can consume a log
// record. The metric sinks (senhub, prtg, http) are absent because they
// take datapoints, not logs — routing a probe's logs to one of them
// would deliver them nowhere.
//
// Kept here rather than in the data store so `agent config check` can
// reject an unroutable value without importing the strategies.
var LogCapableStrategies = []string{"otlp", "event"}

// IsLogCapableStrategy reports whether name is an output that can
// receive log records.
func IsLogCapableStrategy(name string) bool {
	for _, s := range LogCapableStrategies {
		if s == name {
			return true
		}
	}
	return false
}

// IsEnabled reports whether the probe should run. An absent `enabled` key means
// yes, which is what keeps every pre-existing configuration behaving exactly as
// it did.
func (p ProbeConfig) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}

// AgentConfig is the small "identity" block (key, license, version,
// update settings). The pre-0.2.0 RemoteConfiguration also stored a
// RegistryUrl here for SaaS-side update polling; that field is kept
// to avoid a JSON-schema break for callers that still serialize it.
type AgentConfig struct {
	RegistryUrl         string `json:"registry_url"`
	Version             string `json:"version"`
	UpdateCheckInterval any    `json:"update_check_interval" default:"3600"`
	// IncludeBeta mirrors the local auto_update.include_beta flag so the
	// active updater's "latest" resolution can consider the beta channel.
	IncludeBeta       bool   `json:"include_beta"`
	License           string `json:"license,omitempty"`
	AuthenticationKey string `json:"authentication_key,omitempty"`
	// GlobalTags are applied to every datapoint of every probe. Set from the
	// agent.yaml `agent.global_tags` block.
	GlobalTags map[string]string `json:"global_tags,omitempty"`
}

// ConfigurationData is the full configuration shape consumed by
// the agent's data store and sensor pool. The "Remote" prefix is
// historical; in 0.2.0+ the only producer is LocalConfiguration.
//
// TODO(#138): rename ConfigurationData / RemoteConfiguration*
// away from the misleading "Remote" prefix once every consumer
// (data_store, sensor, auto_update, http strategy) is migrated.
// Deferred from the v0.2.0 PR to keep that diff scoped.
type ConfigurationData struct {
	StorageConfig []StorageConfig `json:"storage"`
	Probes        []ProbeConfig   `json:"probes"`
	Agent         AgentConfig     `json:"agent"`
	Cache         *CacheConfig    `json:"cache,omitempty"`
}

// validProbeName matches names safe in URLs and file names: letters,
// digits, hyphens and underscores, not starting with a punctuation mark.
var validProbeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// IsValidProbeName reports whether a probe name may be used: the sensor
// skips a probe whose name fails this, and the configurator refuses it
// before writing.
func IsValidProbeName(name string) bool {
	return name != "" && validProbeName.MatchString(name)
}

// ID is the identity of a configured probe: a hash of its name and
// parameters, so any change to either yields a new probe and a restart
// of that probe alone. The sensor keys its running pollers by it, and
// agentstate publishes their state under it.
func (p ProbeConfig) ID() string {
	input := fmt.Sprintf("%s-%v", p.Name, p.Params)
	hash := sha256.New()
	hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

// ParseGovernance validates the instance's governance block. The zero
// value comes back when none is declared.
func (p ProbeConfig) ParseGovernance() (governance.Governance, error) {
	if p.Governance == nil {
		return governance.Governance{}, nil
	}
	return governance.Parse(p.Governance)
}
