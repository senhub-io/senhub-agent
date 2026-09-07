package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/governance"
)

// probeWriteRequest is the body of a create or update: the same shape
// the listing returns, minus the live state.
type probeWriteRequest struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Enabled *bool                  `json:"enabled"`
	Params  map[string]interface{} `json:"params"`
	// Governance is the instance's optional governance block; an empty
	// object means none, so a form with nothing filled in writes nothing.
	Governance map[string]interface{} `json:"governance,omitempty"`
}

type probeWriteResponse struct {
	Status   string   `json:"status"`
	Path     string   `json:"path,omitempty"`
	Applied  string   `json:"applied,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// handleProbeCreate writes a new managed fragment for a probe instance.
// Everything that would make the file useless is refused before it is
// written: an unknown type, a licence that would not run it, a name
// that is invalid or taken, a params map the schema or the constructor
// rejects, and the legacy layout.
func (h *HTTPSyncStrategy) handleProbeCreate(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	req, ok := h.decodeProbeWrite(w, r)
	if !ok {
		return
	}
	ps, warnings, ok := h.checkProbeWrite(w, agentKey, req)
	if !ok {
		return
	}
	path, err := configuration.CreateProbeFragment(h.agentConfig.GetConfigPath(), req.toConfig(), secretPathsOf(ps))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, probeWriteResponse{Status: "success", Path: path,
		Applied: fmt.Sprintf("probe %q created; the agent starts it on its own", req.Name), Warnings: warnings})
}

// handleProbeUpdate rewrites a managed fragment. The name in the path is
// the identity; the body may carry it too and must then match.
func (h *HTTPSyncStrategy) handleProbeUpdate(w http.ResponseWriter, r *http.Request) {
	agentKey, ok := h.authManager.AuthenticateAndExtract(w, r)
	if !ok {
		return
	}
	name := mux.Vars(r)["name"]
	req, ok := h.decodeProbeWrite(w, r)
	if !ok {
		return
	}
	if req.Name != "" && req.Name != name {
		writeJSONError(w, http.StatusBadRequest, "a probe cannot be renamed; delete it and create it under the new name")
		return
	}
	req.Name = name
	ps, warnings, ok := h.checkProbeWrite(w, agentKey, req)
	if !ok {
		return
	}
	path, err := configuration.UpdateProbeFragment(h.agentConfig.GetConfigPath(), req.toConfig(), secretPathsOf(ps))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, probeWriteResponse{Status: "success", Path: path,
		Applied: fmt.Sprintf("probe %q updated; the agent restarts it on its own", name), Warnings: warnings})
}

// handleProbeDelete removes a managed fragment. Prefer disabling: a
// deleted probe loses its credentials and settings.
func (h *HTTPSyncStrategy) handleProbeDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	name := mux.Vars(r)["name"]
	path, err := configuration.DeleteProbeFragment(h.agentConfig.GetConfigPath(), name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, probeWriteResponse{Status: "success", Path: path,
		Applied: fmt.Sprintf("probe %q removed; the agent stops it on its own", name)})
}

func (h *HTTPSyncStrategy) decodeProbeWrite(w http.ResponseWriter, r *http.Request) (probeWriteRequest, bool) {
	var req probeWriteRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return req, false
	}
	if h.agentConfig.GetConfigPath() == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return req, false
	}
	if req.Params == nil {
		req.Params = map[string]interface{}{}
	}
	return req, true
}

// checkProbeWrite applies, in order, the checks that decide whether the
// fragment is worth writing: known type, licence, schema, constructor.
// It returns the schema (for secret paths) and constructor warnings.
func (h *HTTPSyncStrategy) checkProbeWrite(w http.ResponseWriter, agentKey string, req probeWriteRequest) (spec.Probe, []string, bool) {
	ps, has := spec.For(req.Type)
	if !has {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("probe type %q is not in the catalogue of this agent", req.Type))
		return ps, nil, false
	}
	if verdict := annotateCatalogEntry(ps, h.currentLicense(), agentKey); !verdict.Authorized {
		writeJSONError(w, http.StatusForbidden, fmt.Sprintf("probe type %q: %s", req.Type, verdict.Reason))
		return ps, nil, false
	}
	if problems := ps.CheckParams(req.Params); len(problems) > 0 {
		msgs := make([]string, 0, len(problems))
		for _, p := range problems {
			msgs = append(msgs, p.String())
		}
		writeJSONError(w, http.StatusBadRequest, "parameters: "+strings.Join(msgs, "; "))
		return ps, nil, false
	}
	if err := checkGovernanceBlock(req.Governance); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return ps, nil, false
	}
	var warnings []string
	if ProbeChecker != nil {
		issues, err := ProbeChecker(req.Type, req.Params)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "the probe refuses this configuration: "+err.Error())
			return ps, nil, false
		}
		for _, is := range issues {
			warnings = append(warnings, fmt.Sprintf("param %q reads %v, which is not %s; the probe uses its default", is.Key, is.Got, is.Want))
		}
	}
	return ps, warnings, true
}

func (req probeWriteRequest) toConfig() configuration.ProbeConfig {
	cfg := configuration.ProbeConfig{Name: req.Name, Type: req.Type, Enabled: req.Enabled, Params: req.Params}
	if len(req.Governance) > 0 {
		cfg.Governance = req.Governance
	}
	return cfg
}

// checkGovernanceBlock applies the same two checks `config check` runs:
// the shape against the shared schema, then the values against the
// governance package's closed sets. Nil and empty are fine.
func checkGovernanceBlock(v map[string]interface{}) error {
	if len(v) == 0 {
		return nil
	}
	if problems := spec.CheckGovernance(v); len(problems) > 0 {
		msgs := make([]string, 0, len(problems))
		for _, p := range problems {
			msgs = append(msgs, p.String())
		}
		return errors.New(strings.Join(msgs, "; "))
	}
	if _, err := governance.Parse(v); err != nil {
		return fmt.Errorf("governance: %w", err)
	}
	return nil
}

// secretPathsOf lists the dotted param paths the schema marks secret.
func secretPathsOf(ps spec.Probe) []string {
	var out []string
	var walk func(prefix string, params []spec.ParamSpec)
	walk = func(prefix string, params []spec.ParamSpec) {
		for _, p := range params {
			if p.Secret {
				out = append(out, prefix+p.Key)
			}
			if p.Kind == spec.KindBlock {
				walk(prefix+p.Key+".", p.Fields)
			}
		}
	}
	walk("", ps.Params)
	return out
}
