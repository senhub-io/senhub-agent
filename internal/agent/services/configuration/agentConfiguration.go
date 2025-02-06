// Package configuration manages agent configuration from local and remote sources
package configuration

import (
	"fmt"
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
) AgentConfiguration {
	fmt.Printf("[DEBUG] Creating new AgentConfiguration instance\n")
	ac := &agentConfiguration{
		AuthenticationKey: AuthenticationKey,
		ServerUrl:         ServerUrl,
	}
	fmt.Printf("[DEBUG] AgentConfiguration instance created successfully\n")
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
