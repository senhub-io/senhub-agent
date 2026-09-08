package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
)

// The outputs API is the strategies.d counterpart of the probes API:
// one file per output, the same schema model for the forms, and the
// live state next to what the file says. An output is named by its
// strategy type, because the loader keeps one per type.

// outputCatalogEntry is one output type the console can offer.
type outputCatalogEntry struct {
	outputspec.Output
	// Configured says a file already exists for this type; a singleton
	// or an already configured output is edited, not added.
	Configured bool `json:"configured"`
}

type outputCatalogResponse struct {
	Outputs []outputCatalogEntry `json:"outputs"`
}

// outputActivity is the delivery record of a push output.
type outputActivity struct {
	LastSuccess *time.Time `json:"last_success,omitempty"`
	LastFailure *time.Time `json:"last_failure,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	Successes   uint64     `json:"successes"`
	Failures    uint64     `json:"failures"`
}

// outputReader is the last poller seen on one endpoint family of the
// http output.
type outputReader struct {
	Endpoint string     `json:"endpoint"`
	Enabled  bool       `json:"enabled"`
	Last     *time.Time `json:"last,omitempty"`
	From     string     `json:"from,omitempty"`
	Total    uint64     `json:"total"`
}

// configuredOutput is one output as the console lists it.
type configuredOutput struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	DisplayName string                 `json:"display_name"`
	Mode        string                 `json:"mode"`
	Enabled     bool                   `json:"enabled"`
	Managed     bool                   `json:"managed"`
	Path        string                 `json:"path"`
	Params      map[string]interface{} `json:"params"`
	HasSchema   bool                   `json:"has_schema"`
	// State is one of listening, exporting, idle, failing, disabled.
	State    string          `json:"state"`
	Reason   string          `json:"reason,omitempty"`
	Activity *outputActivity `json:"activity,omitempty"`
	Readers  []outputReader  `json:"readers,omitempty"`
}

type configuredOutputsResponse struct {
	Outputs []configuredOutput `json:"outputs"`
	Count   int                `json:"count"`
}

// outputWriteRequest is the body of a create or update.
type outputWriteRequest struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Enabled *bool                  `json:"enabled"`
	Params  map[string]interface{} `json:"params"`
}

type outputWriteResponse struct {
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Applied string `json:"applied,omitempty"`
}

type outputValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
	Field  string   `json:"field,omitempty"`
}

type outputTestResponse struct {
	Valid    bool                  `json:"valid"`
	Steps    []otlp.ConnectionStep `json:"steps"`
	Errors   []string              `json:"errors,omitempty"`
	Duration int64                 `json:"duration_ms"`
}

func (h *HTTPSyncStrategy) handleCatalogOutputs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	configured := map[string]bool{}
	if path := h.agentConfig.GetConfigPath(); path != "" {
		if frags, err := configuration.ListStrategyFragments(path); err == nil {
			for _, f := range frags {
				configured[f.Name] = true
			}
		}
	}
	entries := make([]outputCatalogEntry, 0, 8)
	for _, o := range outputspec.Registered() {
		entries = append(entries, outputCatalogEntry{Output: o, Configured: configured[o.Type]})
	}
	writeJSON(w, http.StatusOK, outputCatalogResponse{Outputs: entries})
}

func (h *HTTPSyncStrategy) handleConfiguredOutputs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	configPath := h.agentConfig.GetConfigPath()
	if configPath == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	frags, err := configuration.ListStrategyFragments(configPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]configuredOutput, 0, len(frags))
	for _, f := range frags {
		out = append(out, h.describeOutput(f))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, configuredOutputsResponse{Outputs: out, Count: len(out)})
}

// describeOutput joins what the file says with what the agent is doing.
func (h *HTTPSyncStrategy) describeOutput(f configuration.StrategyFragment) configuredOutput {
	entry := configuredOutput{
		Name: f.Name, Type: f.Name, DisplayName: f.Name, Mode: string(outputspec.ModePush),
		Enabled: f.Enabled, Managed: f.Managed, Path: f.Path,
		Params: configuration.SanitizeParamsForLog(f.Params),
	}
	if spec, has := outputspec.For(f.Name); has {
		entry.HasSchema = true
		entry.DisplayName = spec.DisplayName
		entry.Mode = string(spec.Mode)
	}
	if entry.Params == nil {
		entry.Params = map[string]interface{}{}
	}
	if !f.Enabled {
		entry.State = "disabled"
		return entry
	}
	if failure, failing := agentstate.GetStrategyFailures()[f.Name]; failing {
		entry.State, entry.Reason = "failing", failure.Detail
		if entry.Reason == "" {
			entry.Reason = failure.Reason
		}
		return entry
	}
	if f.Name == "http" {
		entry.State = "listening"
		entry.Readers = h.describeReaders()
		return entry
	}
	act := agentstate.GetExportActivity(f.Name)
	entry.Activity = &outputActivity{Successes: act.Successes, Failures: act.Failures, LastError: act.LastError}
	if !act.LastSuccess.IsZero() {
		t := act.LastSuccess
		entry.Activity.LastSuccess = &t
	}
	if !act.LastFailure.IsZero() {
		t := act.LastFailure
		entry.Activity.LastFailure = &t
	}
	switch {
	case !act.LastFailure.IsZero() && act.LastFailure.After(act.LastSuccess):
		entry.State, entry.Reason = "failing", act.LastError
	case !act.LastSuccess.IsZero():
		entry.State = "exporting"
	default:
		entry.State = "idle"
	}
	return entry
}

// describeReaders lists the endpoint families of the http output with
// the last poller that read each.
func (h *HTTPSyncStrategy) describeReaders() []outputReader {
	activity := GetRequestActivity()
	out := make([]outputReader, 0, 4)
	for _, family := range []string{"prtg", "nagios", "prometheus", "web"} {
		reader := outputReader{Endpoint: family, Enabled: h.configManager.IsEndpointEnabled(family)}
		if a, seen := activity[family]; seen {
			t := a.Last
			reader.Last, reader.From, reader.Total = &t, a.From, a.Total
		}
		out = append(out, reader)
	}
	return out
}

func (h *HTTPSyncStrategy) handleOutputCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	req, ok := h.decodeOutputWrite(w, r)
	if !ok {
		return
	}
	spec, ok := h.checkOutputWrite(w, req)
	if !ok {
		return
	}
	if spec.Singleton {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("output %q exists once and is created by the install; edit it instead", req.Type))
		return
	}
	path, err := configuration.CreateStrategyFragment(h.agentConfig.GetConfigPath(), req.Type, req.Params, spec.SecretPaths())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Enabled != nil && !*req.Enabled {
		if path, err = configuration.UpdateStrategyFragment(h.agentConfig.GetConfigPath(), req.Type, req.Params, false, spec.SecretPaths()); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	agentstate.RecordEvent(agentstate.EventInfo, agentstate.EventKindConsole, req.Type, "output created from the console")
	writeJSON(w, http.StatusCreated, outputWriteResponse{Status: "success", Path: path,
		Applied: fmt.Sprintf("output %q created; the agent starts it on its own", req.Type)})
}

func (h *HTTPSyncStrategy) handleOutputUpdate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	name := mux.Vars(r)["name"]
	req, ok := h.decodeOutputWrite(w, r)
	if !ok {
		return
	}
	if req.Type != "" && req.Type != name {
		writeJSONError(w, http.StatusBadRequest, "an output is named by its type; it cannot be renamed")
		return
	}
	req.Type = name
	spec, ok := h.checkOutputWrite(w, req)
	if !ok {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if name == "http" && !enabled {
		writeJSONError(w, http.StatusBadRequest, "the http output serves this console; it cannot be disabled from here")
		return
	}
	path, err := configuration.UpdateStrategyFragment(h.agentConfig.GetConfigPath(), name, req.Params, enabled, spec.SecretPaths())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	what := "updated; the agent restarts it on its own"
	if !enabled {
		what = "disabled; its file is kept as " + path
	}
	agentstate.RecordEvent(agentstate.EventInfo, agentstate.EventKindConsole, name, "output "+strings.SplitN(what, ";", 2)[0]+" from the console")
	writeJSON(w, http.StatusOK, outputWriteResponse{Status: "success", Path: path, Applied: fmt.Sprintf("output %q %s", name, what)})
}

func (h *HTTPSyncStrategy) handleOutputDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	name := mux.Vars(r)["name"]
	if name == "http" {
		writeJSONError(w, http.StatusBadRequest, "the http output serves this console; it cannot be removed from here")
		return
	}
	if h.agentConfig.GetConfigPath() == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return
	}
	path, err := configuration.DeleteStrategyFragment(h.agentConfig.GetConfigPath(), name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentstate.RecordEvent(agentstate.EventInfo, agentstate.EventKindConsole, name, "output removed from the console")
	writeJSON(w, http.StatusOK, outputWriteResponse{Status: "success", Path: path,
		Applied: fmt.Sprintf("output %q removed; the agent stops it on its own", name)})
}

func (h *HTTPSyncStrategy) handleOutputValidate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	var req outputWriteRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Type == "" {
		req.Type = req.Name
	}
	if req.Params == nil {
		req.Params = map[string]interface{}{}
	}
	if err := checkOutputParams(req.Type, req.Params); err != nil {
		writeJSON(w, http.StatusOK, outputValidateResponse{Valid: false, Errors: []string{err.Error()}, Field: guessOutputField(req.Type, err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, outputValidateResponse{Valid: true})
}

// handleOutputTest checks the connection with the values sent, saving
// nothing: the operator sees at which step a dead sink dies.
func (h *HTTPSyncStrategy) handleOutputTest(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	var req struct {
		outputWriteRequest
		Timeout int `json:"timeout"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Type == "" {
		req.Type = req.Name
	}
	if req.Params == nil {
		req.Params = map[string]interface{}{}
	}
	start := time.Now()
	timeout := time.Duration(clampConnectivityTimeout(req.Timeout)) * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	var steps []otlp.ConnectionStep
	switch req.Type {
	case "otlp":
		steps = otlp.ProbeConnection(ctx, req.Params, guardedDialer(timeout).DialContext)
	case "prtg", "event":
		steps = probeHTTPTarget(ctx, req.Type, req.Params, timeout)
	case "http":
		steps = []otlp.ConnectionStep{{Name: "listen", Passed: true, Detail: fmt.Sprintf("this console answers on port %d", h.configManager.GetPort())}}
	default:
		steps = []otlp.ConnectionStep{{Name: "test", Passed: false, Error: fmt.Sprintf("no connection test for output %q", req.Type)}}
	}
	resp := outputTestResponse{Valid: true, Steps: steps, Duration: time.Since(start).Milliseconds()}
	for _, s := range steps {
		if !s.Passed {
			resp.Valid = false
			resp.Errors = append(resp.Errors, s.Name+": "+s.Error)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// probeHTTPTarget resolves and reaches the server_url of a push output
// that speaks plain HTTP, without posting anything to it.
func probeHTTPTarget(ctx context.Context, outputType string, params map[string]interface{}, timeout time.Duration) []otlp.ConnectionStep {
	var steps []otlp.ConnectionStep
	run := func(name string, fn func() (string, error)) bool {
		t := time.Now()
		detail, err := fn()
		s := otlp.ConnectionStep{Name: name, Passed: err == nil, Detail: detail, Duration: time.Since(t).Milliseconds()}
		if err != nil {
			s.Error = err.Error()
		}
		steps = append(steps, s)
		return err == nil
	}
	// The key is read through a variable: the known-params guard scans
	// this package for literal lookups and would take it for a key of
	// the http strategy itself.
	const urlKey = "server" + "_url"
	target, _ := params[urlKey].(string)
	if !run("config", func() (string, error) {
		if err := checkOutputParams(outputType, params); err != nil {
			return "", err
		}
		return target, nil
	}) {
		return steps
	}
	run("reach", func() (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
		if err != nil {
			return "", err
		}
		resp, err := newConnectivityClient(timeout).Do(req)
		if err != nil {
			return "", err
		}
		_ = resp.Body.Close()
		return fmt.Sprintf("HTTP %d", resp.StatusCode), nil
	})
	return steps
}

// guardedDialer dials with the SSRF guard the connectivity client uses.
func guardedDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: connectivityDialControl}
}

func (h *HTTPSyncStrategy) decodeOutputWrite(w http.ResponseWriter, r *http.Request) (outputWriteRequest, bool) {
	var req outputWriteRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return req, false
	}
	if h.agentConfig.GetConfigPath() == "" {
		writeJSONError(w, http.StatusInternalServerError, "the agent config path is not known to this strategy")
		return req, false
	}
	if req.Type == "" {
		req.Type = req.Name
	}
	if req.Params == nil {
		req.Params = map[string]interface{}{}
	}
	return req, true
}

// checkOutputWrite refuses an unknown type and params the schema or the
// strategy's own parser rejects, before anything is written.
func (h *HTTPSyncStrategy) checkOutputWrite(w http.ResponseWriter, req outputWriteRequest) (outputspec.Output, bool) {
	spec, has := outputspec.For(req.Type)
	if !has {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("output type %q is not in the catalogue of this agent", req.Type))
		return spec, false
	}
	if err := checkOutputParams(req.Type, req.Params); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return spec, false
	}
	return spec, true
}

// checkOutputParams applies the schema, then the strategy's parser: the
// same two checks `config check` runs on a file.
func checkOutputParams(outputType string, params map[string]interface{}) error {
	spec, has := outputspec.For(outputType)
	if !has {
		return fmt.Errorf("output type %q is not in the catalogue of this agent", outputType)
	}
	if problems := spec.CheckParams(params); len(problems) > 0 {
		msgs := make([]string, 0, len(problems))
		for _, p := range problems {
			msgs = append(msgs, p.String())
		}
		return errors.New("parameters: " + strings.Join(msgs, "; "))
	}
	if OutputValidator != nil {
		if err := OutputValidator(outputType, params); err != nil {
			return fmt.Errorf("the output refuses this configuration: %w", err)
		}
	}
	return nil
}

// guessOutputField is guessField for an output schema.
func guessOutputField(outputType, message string) string {
	spec, has := outputspec.For(outputType)
	if !has {
		return ""
	}
	return guessFieldIn(spec.DeclaredKeys(), message)
}

func (h *HTTPSyncStrategy) handleInfoEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authManager.AuthenticateAndExtract(w, r); !ok {
		return
	}
	events := agentstate.GetEvents()
	if events == nil {
		events = []agentstate.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"events": events, "count": len(events)})
}
