// Package configuration manages agent configuration from local and remote sources
package configuration

import (
	"context"
	"senhub-agent.go/internal/agent/services/logger"
)

// AgentConfiguration defines interface for accessing local static configuration
// loaded from files, environment variables and CLI args
type AgentConfiguration interface {
	// GetAuthenticationKey returns authentication key for server API
	GetAuthenticationKey() string

	// GetServerUrl returns server endpoint URL
	GetServerUrl() string
}

// ConfigurationProvider defines interface for both remote and local configuration
type ConfigurationProvider interface {
	GetName() string
	GetConfiguration() RemoteConfigurationData
	OnConfigChanged(callback func(string))
	Start(chan struct{}) error
	Shutdown(context.Context) error
}

// agentConfiguration implements AgentConfiguration
type agentConfiguration struct {
	AuthenticationKey   string
	ServerUrl           string
	localConfiguration  *LocalConfiguration  // Optional reference for offline mode
	remoteConfiguration *RemoteConfiguration // Optional reference for online mode
}

// NewAgentConfiguration creates AgentConfiguration instance
func NewAgentConfiguration(
	AuthenticationKey string,
	ServerUrl string,
	baseLogger *logger.Logger,
) AgentConfiguration {
	// Create module-specific logger for agent configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.agent")
	moduleLogger.Debug().Msg("Creating new AgentConfiguration instance")
	ac := &agentConfiguration{
		AuthenticationKey:   AuthenticationKey,
		ServerUrl:           ServerUrl,
		localConfiguration:  nil,
		remoteConfiguration: nil,
	}
	moduleLogger.Debug().Msg("AgentConfiguration instance created successfully")
	return ac
}

// NewAgentConfigurationWithLocal creates AgentConfiguration instance with LocalConfiguration reference
func NewAgentConfigurationWithLocal(
	AuthenticationKey string,
	ServerUrl string,
	localConfig *LocalConfiguration,
	baseLogger *logger.Logger,
) AgentConfiguration {
	// Create module-specific logger for agent configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.agent")
	moduleLogger.Debug().Msg("Creating new AgentConfiguration instance with LocalConfiguration")
	ac := &agentConfiguration{
		AuthenticationKey:   AuthenticationKey,
		ServerUrl:           ServerUrl,
		localConfiguration:  localConfig,
		remoteConfiguration: nil,
	}
	moduleLogger.Debug().Msg("AgentConfiguration instance with LocalConfiguration created successfully")
	return ac
}

// NewAgentConfigurationWithRemote creates AgentConfiguration instance with RemoteConfiguration reference
func NewAgentConfigurationWithRemote(
	AuthenticationKey string,
	ServerUrl string,
	remoteConfig *RemoteConfiguration,
	baseLogger *logger.Logger,
) AgentConfiguration {
	// Create module-specific logger for agent configuration
	moduleLogger := logger.NewModuleLogger(baseLogger, "configuration.agent")
	moduleLogger.Debug().Msg("Creating new AgentConfiguration instance with RemoteConfiguration")
	ac := &agentConfiguration{
		AuthenticationKey:   AuthenticationKey,
		ServerUrl:           ServerUrl,
		localConfiguration:  nil,
		remoteConfiguration: remoteConfig,
	}
	moduleLogger.Debug().Msg("AgentConfiguration instance with RemoteConfiguration created successfully")
	return ac
}

// GetAuthenticationKey returns the authentication key
func (l *agentConfiguration) GetAuthenticationKey() string {
	return l.AuthenticationKey
}

// GetServerUrl returns the server URL
func (l *agentConfiguration) GetServerUrl() string {
	return l.ServerUrl
}

// GetConfiguration returns configuration data from either Local or Remote configuration
func (l *agentConfiguration) GetConfiguration() RemoteConfigurationData {
	if l.localConfiguration != nil {
		return l.localConfiguration.GetConfiguration()
	}
	if l.remoteConfiguration != nil {
		return l.remoteConfiguration.GetConfiguration()
	}
	// Return empty configuration if no source
	return RemoteConfigurationData{}
}

// GetCacheConfig returns cache configuration from either Local or Remote configuration
func (l *agentConfiguration) GetCacheConfig() *CacheConfig {
	if l.localConfiguration != nil {
		return l.localConfiguration.GetCacheConfig()
	}
	if l.remoteConfiguration != nil {
		return l.remoteConfiguration.GetCacheConfig()
	}
	// Return default if no configuration source
	return &CacheConfig{
		RetentionMinutes: 5,
	}
}
