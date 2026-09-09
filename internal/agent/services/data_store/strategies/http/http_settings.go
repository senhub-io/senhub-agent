package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
)

// settingsResponse is the GET view of the settings the web page can change.
type settingsResponse struct {
	AgentKey    string      `json:"agent_key"`
	Port        int         `json:"port"`
	BindAddress string      `json:"bind_address"`
	TLSEnabled  bool        `json:"tls_enabled"`
	License     licenseView `json:"license"`
}

type licenseView struct {
	Configured bool   `json:"configured"`
	Tier       string `json:"tier,omitempty"`
	Bound      bool   `json:"bound"`
	Scope      string `json:"scope,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Expired    bool   `json:"expired,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// settingsRequest is the POST body; every field is optional so the page can
// send only what changed. A pointer distinguishes "leave unchanged" (nil)
// from a zero value.
type settingsRequest struct {
	Port        *int    `json:"port"`
	BindAddress *string `json:"bind_address"`
	License     *string `json:"license"`
}

type settingsResult struct {
	Applied  []string `json:"applied"`
	Warnings []string `json:"warnings"`
	Status   string   `json:"status"`
}

// handleWebSettings renders the settings page.
func (h *HTTPSyncStrategy) handleWebSettings(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	assetHandler := NewAssetHandlerWithPRTG(agentKey, h.configManager.IsEndpointEnabled("prtg"))
	content, err := assetHandler.RenderTemplate("settings")
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to render settings template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if _, err := w.Write([]byte(content)); err != nil {
		h.logger.Error().Err(err).Msg("Failed to write settings page")
	}
}

// handleWebPage renders one of the embedded console pages.
func (h *HTTPSyncStrategy) handleWebPage(w http.ResponseWriter, r *http.Request, template string) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	content, err := NewAssetHandlerWithPRTG(agentKey, h.configManager.IsEndpointEnabled("prtg")).RenderTemplate(template)
	if err != nil {
		h.logger.Error().Err(err).Str("template", template).Msg("Failed to render console page")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if _, err := w.Write([]byte(content)); err != nil {
		h.logger.Error().Err(err).Msg("Failed to write console page")
	}
}

// handleConfigSettingsGet returns the current, editable settings as JSON.
func (h *HTTPSyncStrategy) handleConfigSettingsGet(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	resp := settingsResponse{
		AgentKey:    agentKey,
		Port:        h.configManager.GetPort(),
		BindAddress: h.configManager.GetBindAddress(),
		TLSEnabled:  h.configManager.IsTLSEnabled(),
		License:     h.currentLicenseView(agentKey),
	}
	writeJSON(w, http.StatusOK, resp)
}

// currentLicenseView resolves the effective licence and reports its tier,
// binding and expiry for display.
func (h *HTTPSyncStrategy) currentLicenseView(agentKey string) licenseView {
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		return licenseView{Detail: "no config path"}
	}
	effective, err := configuration.ResolveEffectiveLicense(configPath, "")
	if err != nil || strings.TrimSpace(effective) == "" {
		return licenseView{Configured: false}
	}
	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		return licenseView{Configured: true, Detail: "validator unavailable"}
	}
	lic, err := validator.ValidateLicense(effective)
	if err != nil {
		return licenseView{Configured: true, Detail: "invalid licence on disk"}
	}
	var scope string
	switch {
	case lic.Subject == "":
		scope = "valid on any agent"
	case license.SubjectIsAgentKey(lic.Subject):
		if lic.Subject == agentKey {
			scope = "bound to this agent"
		} else {
			scope = "issued for another agent"
		}
	default:
		scope = "customer licence (" + lic.Subject + ")"
	}
	return licenseView{
		Configured: true,
		Tier:       string(lic.Tier),
		Bound:      license.VerifyBinding("", agentKey, lic),
		Scope:      scope,
		ExpiresAt:  lic.ExpiresAt.Format("2006-01-02"),
		Expired:    lic.IsExpired,
	}
}

// handleConfigSettingsSet applies changed settings. Each field is validated
// before it is written; the config watcher reloads the change, so the HTTP
// listener moves to a new port on its own.
func (h *HTTPSyncStrategy) handleConfigSettingsSet(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	result := settingsResult{Status: "success"}

	if req.Port != nil {
		port := *req.Port
		if port < 1 || port > 65535 {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("port must be between 1 and 65535, got %d", port))
			return
		}
		if err := configuration.SetStrategyScalar(configPath, "http", "port", strconv.Itoa(port), "!!int"); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("setting port: %v", err))
			return
		}
		result.Applied = append(result.Applied, fmt.Sprintf("port set to %d", port))
		result.Warnings = append(result.Warnings, fmt.Sprintf("the console and the PRTG / Nagios endpoints move to port %d; reconnect on the new address", port))
	}

	if req.BindAddress != nil {
		bind := strings.TrimSpace(*req.BindAddress)
		if bind == "" {
			writeJSONError(w, http.StatusBadRequest, "bind_address must not be empty")
			return
		}
		if err := configuration.SetStrategyScalar(configPath, "http", "bind_address", bind, "!!str"); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("setting bind_address: %v", err))
			return
		}
		result.Applied = append(result.Applied, "bind_address set to "+bind)
	}

	if req.License != nil {
		jwt := strings.TrimSpace(*req.License)
		if jwt == "" {
			writeJSONError(w, http.StatusBadRequest, "licence must not be empty")
			return
		}
		validator, err := license.GetDefaultValidator(7)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "licence validator unavailable")
			return
		}
		lic, err := validator.ValidateLicense(jwt)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid licence: %v", err))
			return
		}
		if !license.VerifyBinding("", agentKey, lic) {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("this licence is issued for another agent (%q), not this one (%q); use your customer licence", lic.Subject, agentKey))
			return
		}
		if err := configuration.WriteLicenseSidecar(configPath, jwt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("saving licence: %v", err))
			return
		}
		result.Applied = append(result.Applied, "licence activated (tier "+string(lic.Tier)+")")
	}

	if len(result.Applied) == 0 {
		writeJSONError(w, http.StatusBadRequest, "nothing to change")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// writeJSON encodes before it writes the status: a value the encoder
// refuses (a map keyed by interface{}, a channel) must come back as a 500
// that names the problem, not as a 200 with an empty body the page then
// fails to parse without a clue.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body, _ = json.Marshal(map[string]string{"status": "error", "error": "response could not be encoded: " + err.Error()})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"status": "error", "error": msg})
}
