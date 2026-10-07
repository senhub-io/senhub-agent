package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/governance"
)

// agentGovernanceView is what the Settings page shows of the agent-level
// governance block: the values as the file holds them, and whether the
// page may change them.
type agentGovernanceView struct {
	Governance map[string]interface{} `json:"governance"`
	// ReadOnly is set when a value is a reference (${env:}, ${file:},
	// ${secret:}): writing the resolved value would replace the
	// reference, so the change is made where the reference points.
	ReadOnly bool     `json:"read_only"`
	Reason   string   `json:"reason,omitempty"`
	Fields   []string `json:"referenced_fields,omitempty"`
	// Applies says what a save does to the running agent.
	Applies string `json:"applies"`
}

const governanceAppliesLive = "the agent follows a saved change on its own, without a restart"

type agentGovernanceRequest struct {
	Governance map[string]interface{} `json:"governance"`
}

type agentGovernanceError struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	// Fields maps the dotted path of each refused field (the paths the
	// probe form uses, governance.criticality) to what is wrong with it.
	Fields map[string]string `json:"fields,omitempty"`
}

func governanceReadOnlyReason(refs []string) string {
	return fmt.Sprintf("set from environment variables or files (%s); change them where the agent is started, not here", strings.Join(refs, ", "))
}

// handleConfigGovernanceGet returns the agent-level governance block.
func (h *HTTPSyncStrategy) handleConfigGovernanceGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	block, err := configuration.ReadAgentGovernance(configPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if block == nil {
		block = map[string]interface{}{}
	}
	view := agentGovernanceView{Governance: block, Applies: governanceAppliesLive}
	if refs := configuration.GovernanceReferences(block); len(refs) > 0 {
		view.ReadOnly = true
		view.Fields = refs
		view.Reason = governanceReadOnlyReason(refs)
	}
	writeJSON(w, http.StatusOK, view)
}

// handleConfigGovernanceSet replaces the agent-level governance block.
// The block gets the checks `config check` runs, and a refusal names the
// field so the form can mark it.
func (h *HTTPSyncStrategy) handleConfigGovernanceSet(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	var req agentGovernanceRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	current, err := configuration.ReadAgentGovernance(configPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if refs := configuration.GovernanceReferences(current); len(refs) > 0 {
		writeJSONError(w, http.StatusConflict, "governance is "+governanceReadOnlyReason(refs))
		return
	}

	block := pruneGovernance(req.Governance)
	if fields := governanceFieldErrors(block); len(fields) > 0 {
		msgs := make([]string, 0, len(fields))
		for k, m := range fields {
			msgs = append(msgs, k+": "+m)
		}
		writeJSON(w, http.StatusBadRequest, agentGovernanceError{Status: "error", Error: strings.Join(msgs, "; "), Fields: fields})
		return
	}
	if err := configuration.SetAgentGovernance(configPath, block); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if block == nil {
		block = map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "success",
		"governance": block,
		"applied":    "governance saved; " + governanceAppliesLive,
	})
}

// governanceFieldErrors runs the schema check, then the closed sets, and
// returns one message per refused dotted path.
func governanceFieldErrors(block map[string]interface{}) map[string]string {
	fields := map[string]string{}
	if len(block) == 0 {
		return fields
	}
	for _, p := range spec.CheckGovernance(block) {
		fields[p.Key] = p.Message
	}
	if len(fields) > 0 {
		return fields
	}
	if _, err := governance.Parse(block); err != nil {
		key := "governance"
		if strings.Contains(err.Error(), "criticality") {
			key = "governance.criticality"
		}
		fields[key] = err.Error()
	}
	return fields
}

// pruneGovernance drops empty strings and empty blocks, so a form with
// nothing filled in writes nothing, and trims what is kept. Nil when
// nothing is left.
func pruneGovernance(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				out[k] = s
			}
		case map[string]interface{}:
			if sub := pruneGovernance(t); len(sub) > 0 {
				out[k] = sub
			}
		default:
			if v != nil {
				out[k] = v
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
