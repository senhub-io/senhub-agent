// senhub-agent/internal/agent/services/data_store/http_auth.go
package http

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// AuthenticationManager handles all authentication logic for HTTP endpoints.
//
// Two surfaces, two privileges. The agent key is what a monitoring tool
// is given to read this agent: PRTG, Nagios, a Prometheus scrape. The
// administration key is what changes the agent: its configuration, its
// log levels, its cache, the values it holds. Until they were told
// apart, the key handed to a poller also cleared the cache and injected
// metrics into it — a read-only consumer holding the means to falsify
// what it reads, and the key travels in the URL path, so it lands in
// every access log between the two.
//
// The administration key carries the read privilege too: the web
// console reads as much as it writes, and asking an operator to juggle
// two keys in one page would only invite them to share the stronger one.
type AuthenticationManager struct {
	logger      *logger.ModuleLogger
	agentKey    string
	adminKey    string
	agentConfig configuration.AgentConfiguration
}

// NewAuthenticationManager creates a new authentication manager. An
// empty adminKey leaves the administration surface unauthenticatable,
// which is why the routes are not registered at all in that case.
func NewAuthenticationManager(agentKey, adminKey string, agentConfig configuration.AgentConfiguration, logger *logger.ModuleLogger) *AuthenticationManager {
	return &AuthenticationManager{
		logger:      logger,
		agentKey:    agentKey,
		adminKey:    adminKey,
		agentConfig: agentConfig,
	}
}

// ValidateAgentKey validates the provided key for the READ surface: the
// agent key, or the administration key, which carries the read
// privilege too. The comparison is constant-time
// (validateKeyConstantTime) so the path-key routes get the same
// timing-safe check as the /metrics scrape route.
func (a *AuthenticationManager) ValidateAgentKey(providedKey string) bool {
	return a.validateKeyConstantTime(providedKey) || a.ValidateAdminKey(providedKey)
}

// ValidateAdminKey validates the provided key for the ADMINISTRATION
// surface. Only the administration key opens it; the key a poller holds
// does not. An unset administration key matches nothing — an empty
// configured key must never turn an empty request into a valid one.
func (a *AuthenticationManager) ValidateAdminKey(providedKey string) bool {
	if a.adminKey == "" {
		return false
	}
	return constantTimeEqual(providedKey, a.adminKey)
}

// AdminEnabled reports whether an administration key is configured. The
// caller does not register the administration routes without one: a
// surface nobody can authenticate to is better absent than answering
// Unauthorized, and its absence is what a PRTG-only installation wants.
func (a *AuthenticationManager) AdminEnabled() bool { return a.adminKey != "" }

// AuthenticateAdmin is AuthenticateAndExtract for the administration
// surface: it answers 401 unless the administration key was given.
func (a *AuthenticationManager) AuthenticateAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := mux.Vars(r)["agentkey"]
	if key == "" || !a.ValidateAdminKey(key) {
		a.logger.Warn().
			Str("provided_key_prefix", keyPrefixForLog(key)).
			Str("path", r.URL.Path).
			Msg("Administration request without the administration key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return key, true
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
		// Log only a short prefix: a typo'd REAL key is the common case, and the
		// full value must not land in shared logs.
		a.logger.Warn().
			Str("provided_key_prefix", keyPrefixForLog(agentKey)).
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

// AuthenticateBearerOrQuery validates a request via Authorization: Bearer header
// OR ?token= query parameter. Used by the standard Prometheus scrape route
// `/metrics` which does not embed the agent key in the URL path.
//
// Returns true on success and writes 401 on failure. Returning the agent
// key here would risk it leaking into caller logs or error responses, so
// the bool is intentionally minimal.
//
// Comparison is constant-time to avoid timing attacks.
func (a *AuthenticationManager) AuthenticateBearerOrQuery(w http.ResponseWriter, r *http.Request) bool {
	// Try Authorization: Bearer <token>
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		const prefix = "Bearer "
		if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
			token := authHeader[len(prefix):]
			if a.validateKeyConstantTime(token) {
				return true
			}
			a.logger.Warn().Msg("Invalid Bearer token on Prometheus scrape route")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return false
		}
	}

	// Fallback: ?token=<token> query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		if a.validateKeyConstantTime(token) {
			return true
		}
		a.logger.Warn().Msg("Invalid query-param token on Prometheus scrape route")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}

	a.logger.Warn().Msg("No Bearer token or ?token= on Prometheus scrape route")
	w.Header().Set("WWW-Authenticate", `Bearer realm="senhub-agent"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return false
}

// validateKeyConstantTime compares a provided key against the configured
// agent key(s) using constant-time comparison to avoid timing attacks.
func (a *AuthenticationManager) validateKeyConstantTime(provided string) bool {
	// We intentionally check both keys even if one matches, to avoid leaking
	// which one matched via timing.
	match1 := constantTimeEqual(provided, a.agentKey)
	match2 := false
	if a.agentConfig != nil {
		match2 = constantTimeEqual(provided, a.agentConfig.GetAuthenticationKey())
	}
	return match1 || match2
}

// constantTimeEqual is a constant-time string comparison. Returns false if
// the strings differ in length or content, without short-circuiting on the
// first differing byte.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// UpdateAgentKey updates the agent key (useful for configuration changes).
// Logs only a short prefix of the old/new keys; bounds-safe for short test
// or development keys (< 8 bytes).
func (a *AuthenticationManager) UpdateAgentKey(newKey string) {
	a.logger.Info().
		Str("old_key_prefix", keyPrefixForLog(a.agentKey)).
		Str("new_key_prefix", keyPrefixForLog(newKey)).
		Msg("Agent key updated")

	a.agentKey = newKey
}

// keyPrefixForLog returns at most the first 8 bytes of a key, with a
// trailing "..." marker. Safe for keys of any length, including empty.
func keyPrefixForLog(key string) string {
	const n = 8
	if len(key) == 0 {
		return "(empty)"
	}
	if len(key) <= n {
		// Don't reveal a short key in full — collapse to a length hint.
		return fmt.Sprintf("(short:%dB)", len(key))
	}
	return key[:n] + "..."
}

// adminKeyFrom reads the administration key from the output's
// parameters. Absent or empty leaves the administration surface off,
// which is the default an installation feeding PRTG or Nagios wants: it
// never needed that surface, and until now it carried it anyway.
func adminKeyFrom(params map[string]interface{}) string {
	if params == nil {
		return ""
	}
	v, ok := params["admin_key"]
	if !ok {
		return ""
	}
	key, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}
