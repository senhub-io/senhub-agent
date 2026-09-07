package http

import (
	"net/http"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

// configuredProbe is one probe instance as the configurator lists it:
// what the file says, whether the console owns that file, and what the
// agent is doing with it right now.
type configuredProbe struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Enabled    bool                   `json:"enabled"`
	Params     map[string]interface{} `json:"params"`
	Governance map[string]interface{} `json:"governance,omitempty"`
	Managed    bool                   `json:"managed"`
	Running    bool                   `json:"running"`
	Health     string                 `json:"health,omitempty"`
	Tier       string                 `json:"tier"`
	Authorized bool                   `json:"authorized"`
	Reason     string                 `json:"reason,omitempty"`
	HasSchema  bool                   `json:"has_schema"`
}

type configuredProbesResponse struct {
	Probes []configuredProbe `json:"probes"`
	Count  int               `json:"count"`
}

// handleConfiguredProbes lists the probes the configuration on disk
// declares, with their live state. It replaces a listing derived from
// the metric cache, which could only name probes that had emitted
// something and knew nothing about a probe that failed to start.
func (h *HTTPSyncStrategy) handleConfiguredProbes(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	cfg, err := configuration.LoadFromDisk(configPath, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "loading the configuration: "+err.Error())
		return
	}
	lic := h.currentLicense()
	out := make([]configuredProbe, 0, len(cfg.Probes))
	for _, p := range cfg.Probes {
		state := agentstate.GetProbeRunState(p.ID())
		entry := configuredProbe{
			Name:       p.Name,
			Type:       p.Type,
			Enabled:    p.IsEnabled(),
			Params:     configuration.SanitizeParamsForLog(p.Params),
			Governance: p.Governance,
			Managed:    configuration.IsManagedProbeFragment(configuration.ProbeFragmentPath(configPath, p.Name)),
			Running:    state.Running,
			Health:     state.Health,
			Tier:       "free",
			Authorized: true,
		}
		if ps, has := spec.For(p.Type); has {
			entry.HasSchema = true
			verdict := annotateCatalogEntry(ps, lic, agentKey)
			entry.Tier, entry.Authorized, entry.Reason = verdict.Tier, verdict.Authorized, verdict.Reason
		} else {
			verdict := annotateCatalogEntry(spec.Probe{Type: p.Type}, lic, agentKey)
			entry.Tier, entry.Authorized, entry.Reason = verdict.Tier, verdict.Authorized, verdict.Reason
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, configuredProbesResponse{Probes: out, Count: len(out)})
}
