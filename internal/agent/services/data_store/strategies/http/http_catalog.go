package http

import (
	"net/http"
	"runtime"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
)

// catalogEntry is one probe type the configurator can offer, with the
// licence verdict the form needs to grey it out honestly rather than let
// the operator configure a probe the agent will refuse to start.
type catalogEntry struct {
	spec.Probe
	Tier       string `json:"tier"`
	Authorized bool   `json:"authorized"`
	Reason     string `json:"reason,omitempty"`
}

type catalogResponse struct {
	Probes  []catalogEntry `json:"probes"`
	License licenseView    `json:"license"`
	// Governance is the schema of the block every instance accepts, so the
	// page renders it once, outside the per-type parameters.
	Governance []spec.ParamSpec `json:"governance"`
}

// handleCatalogProbes lists the probe types that declare a schema, each
// with its tier and whether this agent's licence authorises it.
func (h *HTTPSyncStrategy) handleCatalogProbes(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	lic := h.currentLicense()
	view := h.currentLicenseView(agentKey)
	var entries []catalogEntry
	for _, ps := range spec.Registered() {
		entries = append(entries, annotateCatalogEntry(ps, lic, agentKey))
	}
	if entries == nil {
		entries = []catalogEntry{}
	}
	writeJSON(w, http.StatusOK, catalogResponse{Probes: entries, License: view, Governance: spec.GovernanceFields()})
}

// currentLicense returns the validated licence on disk, or nil when none
// is configured or it does not validate. The Free tier is the answer in
// both cases; the settings page explains which.
func (h *HTTPSyncStrategy) currentLicense() *license.License {
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		return nil
	}
	effective, err := configuration.ResolveEffectiveLicense(configPath, "")
	if err != nil || strings.TrimSpace(effective) == "" {
		return nil
	}
	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		return nil
	}
	lic, err := validator.ValidateLicense(effective)
	if err != nil {
		return nil
	}
	return lic
}

// annotateCatalogEntry applies the same rules the sensor applies at
// start: free-tier probes always run; a paid probe needs a licence that
// is valid, bound to this agent, not expired, and that authorises it.
func annotateCatalogEntry(ps spec.Probe, lic *license.License, agentKey string) catalogEntry {
	e := catalogEntry{Probe: ps, Tier: "free", Authorized: true}
	if !ps.RunsOn(runtime.GOOS) {
		e.Authorized = false
		e.Reason = platformReason(ps.Platforms)
		if license.IsProbeAuthorizable(ps.Type) && !isFreeTier(ps.Type) {
			e.Tier = "pro"
		}
		return e
	}
	if license.IsProbeAuthorizable(ps.Type) && !isFreeTier(ps.Type) {
		e.Tier = "pro"
		switch {
		case lic == nil:
			e.Authorized = false
			e.Reason = "requires a licence"
		case !license.VerifyBinding("", agentKey, lic):
			e.Authorized = false
			e.Reason = "the licence is issued for another agent"
		case lic.IsExpired:
			e.Authorized = false
			e.Reason = "the licence has expired"
		default:
			validator, err := license.GetDefaultValidator(7)
			if err != nil || !validator.IsProbeAuthorized(lic, ps.Type) {
				e.Authorized = false
				e.Reason = "not covered by this licence"
			}
		}
	}
	return e
}

// platformReason says where the probe runs, in words an operator reads.
func platformReason(platforms []string) string {
	names := map[string]string{"windows": "Windows", "linux": "Linux", "darwin": "macOS"}
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		if n, ok := names[p]; ok {
			out = append(out, n)
		} else {
			out = append(out, p)
		}
	}
	return strings.Join(out, " and ") + " only"
}

func isFreeTier(probeType string) bool {
	for _, p := range license.GetFreeTierProbes() {
		if p == probeType {
			return true
		}
	}
	return false
}
