// Package configuration manages agent configuration from local and remote sources
package configuration

import (
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

// agentConfiguration implements AgentConfiguration
type agentConfiguration struct {
	AuthenticationKey string
	ServerUrl         string
}

// NewAgentConfiguration creates AgentConfiguration instance
func NewAgentConfiguration(
	AuthenticationKey string,
	ServerUrl string,
	logger *logger.Logger,
) AgentConfiguration {
	localLogger := logger.With().Str("service", "AgentConfiguration").Logger()
	localLogger.Debug().Msg("Creating new AgentConfiguration instance")
	ac := &agentConfiguration{
		AuthenticationKey: AuthenticationKey,
		ServerUrl:         ServerUrl,
	}
	localLogger.Debug().Msg("AgentConfiguration instance created successfully")
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
