// Package configuration manages agent configuration loaded from local
// YAML files. The pre-0.2.0 remote-configuration variant has been
// removed alongside online mode; this file now exposes a single
// LocalConfiguration-backed implementation.
package configuration

import (
	"context"

	"senhub-agent.go/internal/agent/services/logger"
)

// AgentConfiguration exposes the small set of identity fields the
// data store and strategies need to know about the running agent.
// Currently a single read-only accessor; the interface is kept (vs a
// bare struct) so test code can substitute a fake without dragging in
// the whole LocalConfiguration machinery.
type AgentConfiguration interface {
	// GetAuthenticationKey returns the agent's stable identity key,
	// generated at first install and persisted in the config file.
	GetAuthenticationKey() string
}

// ConfigurationProvider is the interface the data store / sensor pool
// observe to react to runtime config changes. Today LocalConfiguration
// is the only implementation; pre-0.2.0 there was also a remote
// variant that fetched from intake.senhub.io.
type ConfigurationProvider interface {
	GetName() string
	GetConfiguration() RemoteConfigurationData
	OnConfigChanged(callback func(string))
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

// agentConfiguration is the concrete AgentConfiguration backed by a
// LocalConfiguration instance.
type agentConfiguration struct {
	AuthenticationKey  string
	localConfiguration *LocalConfiguration
}

// NewAgentConfiguration creates a bare AgentConfiguration with just an
// agent key — no bound configuration source. Useful for tests that
// only need to satisfy the AgentConfiguration interface; production
// code paths use NewAgentConfigurationWithLocal.
//
// The unused middle parameter is kept so the pre-0.2.0 three-argument
// signature still compiles. It used to carry the server URL, now
// ignored — the cloud intake URL is build-time-injected (see
// cliArgs.CloudIntakeURL).
func NewAgentConfiguration(
	authenticationKey string,
	_ string,
	baseLogger interface{},
) AgentConfiguration {
	// Accept zerolog.Logger or *logger.Logger transparently. Tests
	// pass *zerolog.Logger directly; production callers no longer go
	// through this constructor.
	return &agentConfiguration{
		AuthenticationKey: authenticationKey,
	}
}

// NewAgentConfigurationWithLocal binds the agent's identity key to a
// LocalConfiguration source. Pre-0.2.0 there was also a
// NewAgentConfigurationWithRemote variant; that one was deleted with
// online mode.
func NewAgentConfigurationWithLocal(
	authenticationKey string,
	localConfig *LocalConfiguration,
	baseLogger *logger.Logger,
) AgentConfiguration {
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.agent")
	moduleLogger.Debug().Msg("Creating AgentConfiguration bound to LocalConfiguration")
	return &agentConfiguration{
		AuthenticationKey:  authenticationKey,
		localConfiguration: localConfig,
	}
}

func (l *agentConfiguration) GetAuthenticationKey() string {
	return l.AuthenticationKey
}

// GetConfiguration returns the active configuration snapshot from the
// bound LocalConfiguration. Kept for callers that received an
// AgentConfiguration via the legacy constructor signature and need
// access to the merged probes/storage state without holding a direct
// LocalConfiguration reference.
func (l *agentConfiguration) GetConfiguration() RemoteConfigurationData {
	if l.localConfiguration != nil {
		return l.localConfiguration.GetConfiguration()
	}
	return RemoteConfigurationData{}
}

// GetCacheConfig returns the cache block from the bound configuration,
// falling back to a sensible default when no source is bound.
func (l *agentConfiguration) GetCacheConfig() *CacheConfig {
	if l.localConfiguration != nil {
		return l.localConfiguration.GetCacheConfig()
	}
	return &CacheConfig{
		RetentionMinutes: 5,
	}
}
