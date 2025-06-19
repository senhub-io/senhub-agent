// senhub-agent/internal/agent/services/data_store/http_auth.go
package http

import (
	"net/http"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// AuthenticationManager handles all authentication logic for HTTP endpoints
type AuthenticationManager struct {
	logger      *logger.ModuleLogger
	agentKey    string
	agentConfig configuration.AgentConfiguration
}

// NewAuthenticationManager creates a new authentication manager
func NewAuthenticationManager(agentKey string, agentConfig configuration.AgentConfiguration, logger *logger.ModuleLogger) *AuthenticationManager {
	return &AuthenticationManager{
		logger:      logger,
		agentKey:    agentKey,
		agentConfig: agentConfig,
	}
}

// ValidateAgentKey validates the provided agent key against the configured key
func (a *AuthenticationManager) ValidateAgentKey(providedKey string) bool {
	// Primary validation against HTTP strategy agent key
	if providedKey == a.agentKey {
		return true
	}

	// Fallback validation against agent config (for backward compatibility)
	if a.agentConfig != nil && providedKey == a.agentConfig.GetAuthenticationKey() {
		return true
	}

	return false
}

// AuthenticateRequest extracts the agent key from the request and validates it
func (a *AuthenticationManager) AuthenticateRequest(r *http.Request) (string, bool) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]

	if agentKey == "" {
		a.logger.Warn().Msg("Missing agent key in request")
		return "", false
	}

	if !a.ValidateAgentKey(agentKey) {
		a.logger.Warn().
			Str("provided_key", agentKey).
			Msg("Invalid agent key provided")
		return agentKey, false
	}

	return agentKey, true
}

// RequireAuthentication is a middleware that enforces authentication for HTTP handlers
func (a *AuthenticationManager) RequireAuthentication(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, authenticated := a.AuthenticateRequest(r)
		if !authenticated {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// AuthenticateAndExtract authenticates the request and extracts the agent key
// Returns the agent key and a boolean indicating success
func (a *AuthenticationManager) AuthenticateAndExtract(w http.ResponseWriter, r *http.Request) (string, bool) {
	agentKey, authenticated := a.AuthenticateRequest(r)
	if !authenticated {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}

	return agentKey, true
}

// GetAgentKey returns the configured agent key
func (a *AuthenticationManager) GetAgentKey() string {
	return a.agentKey
}

// UpdateAgentKey updates the agent key (useful for configuration changes)
func (a *AuthenticationManager) UpdateAgentKey(newKey string) {
	a.logger.Info().
		Str("old_key_prefix", a.agentKey[:8]+"...").
		Str("new_key_prefix", newKey[:8]+"...").
		Msg("Agent key updated")

	a.agentKey = newKey
}
