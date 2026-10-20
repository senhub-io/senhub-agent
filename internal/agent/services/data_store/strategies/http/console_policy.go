package http

import (
	"fmt"
	"net/http"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
)

// ConsolePolicy is what the console and its API may change on this
// agent. It is read from the `console` block of the http output and
// from there only: the API refuses to edit that block, so an
// administration key cannot widen what it is allowed to do.
type ConsolePolicy struct {
	ReadOnly  bool
	AllowExec bool
}

// DefaultConsolePolicy is today's behaviour: everything the
// administration key could always do.
var DefaultConsolePolicy = ConsolePolicy{AllowExec: true}

// failClosedConsolePolicy applies when the block cannot be read: a
// misspelt switch must not open what its author meant to close.
var failClosedConsolePolicy = ConsolePolicy{ReadOnly: true}

// ParseConsolePolicy reads the `console` block of the http output's
// parameters. An absent block is the default policy.
func ParseConsolePolicy(params map[string]interface{}) (ConsolePolicy, error) {
	raw, present := params["console"]
	if !present || raw == nil {
		return DefaultConsolePolicy, nil
	}
	block, ok := stringKeyed(raw)
	if !ok {
		return failClosedConsolePolicy, fmt.Errorf("console must be a block with read_only and allow_exec, got %T", raw)
	}
	policy := DefaultConsolePolicy
	for key, value := range block {
		b, isBool := value.(bool)
		switch key {
		case "read_only":
			if !isBool {
				return failClosedConsolePolicy, fmt.Errorf("console.read_only must be true or false, got %v", value)
			}
			policy.ReadOnly = b
		case "allow_exec":
			if !isBool {
				return failClosedConsolePolicy, fmt.Errorf("console.allow_exec must be true or false, got %v", value)
			}
			policy.AllowExec = b
		default:
			return failClosedConsolePolicy, fmt.Errorf("console.%s is not a known setting (read_only, allow_exec)", key)
		}
	}
	return policy, nil
}

// ExecAllowed reports whether a write may make the agent run a program.
// A read-only console allows none: it also stops the test endpoints from
// running a probe the caller described.
func (p ConsolePolicy) ExecAllowed() bool { return p.AllowExec && !p.ReadOnly }

// Mode names the effective mode in one word.
func (p ConsolePolicy) Mode() string {
	switch {
	case p.ReadOnly:
		return "read-only"
	case !p.AllowExec:
		return "no-exec"
	default:
		return "full"
	}
}

// Describe is the sentence `config check` and doctor print.
func (p ConsolePolicy) Describe() string {
	switch p.Mode() {
	case "read-only":
		return "console is read-only: every change through the console and its API is refused"
	case "no-exec":
		return "console may change the configuration but not make the agent run a program (exec probe, program paths)"
	default:
		return "console may change the configuration (read_only: false, allow_exec: true)"
	}
}

func stringKeyed(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out, true
	}
	return nil, false
}

// Console returns the policy in force.
func (cm *ConfigurationManager) Console() ConsolePolicy {
	cm.consoleMu.RLock()
	defer cm.consoleMu.RUnlock()
	return cm.console
}

func (cm *ConfigurationManager) loadConsolePolicy() {
	policy, err := ParseConsolePolicy(cm.params)
	if err != nil {
		cm.logger.Warn().Err(err).Msg("Invalid console block, the console is read-only until it is fixed")
	}
	cm.consoleMu.Lock()
	cm.console = policy
	cm.consoleMu.Unlock()
}

// writes lets a route that changes the agent answer only when the
// console is not read-only.
func (h *HTTPHandlers) writes(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.strategy.configManager.Console().ReadOnly {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"status": "error",
				"code":   "console_read_only",
				"error":  "this agent's console is read-only (console.read_only in the http output); change the configuration files, not the API",
			})
			return
		}
		next(w, r)
	}
}

// execRefusal returns why a probe configuration may not be written or
// run through the console, or "" when it may. stored is what the file
// already holds for the instance: an executable path left as it is
// stays acceptable, so an operator can still edit the rest of a probe
// whose path the file sets.
func (p ConsolePolicy) execRefusal(ps spec.Probe, params, stored map[string]interface{}) string {
	if p.ExecAllowed() {
		return ""
	}
	why := "this agent's console is read-only"
	if !p.ReadOnly {
		why = "console.allow_exec is false on this agent"
	}
	if ps.Type == "exec" {
		return fmt.Sprintf("%s: the console cannot create, change or test an exec probe", why)
	}
	for path, def := range ps.ExecutablePaths() {
		value, _ := lookupPath(params, path).(string)
		value = strings.TrimSpace(value)
		if value == "" || value == def {
			continue
		}
		if kept, _ := lookupPath(stored, path).(string); kept == value {
			continue
		}
		return fmt.Sprintf("%s: the console cannot set %s, which names a program the agent would run", why, path)
	}
	return ""
}

func lookupPath(params map[string]interface{}, path string) interface{} {
	var cur interface{} = params
	for _, key := range strings.Split(path, ".") {
		m, ok := stringKeyed(cur)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

// pinConsolePolicy keeps the console block out of the API's reach. A
// request may carry the block the console displayed, unchanged; one that
// changes it is refused, and what the file holds is put back so that
// rewriting the http output never drops it.
func pinConsolePolicy(w http.ResponseWriter, outputType string, params, stored map[string]interface{}) bool {
	if outputType != "http" {
		return true
	}
	incoming, sent := params["console"]
	if sent && incoming != nil {
		have, _ := ParseConsolePolicy(stored)
		want, err := ParseConsolePolicy(params)
		if err != nil || want != have {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"status": "error",
				"code":   "console_policy_file_only",
				"error":  "the console block of the http output is set in its file only; neither the console nor the API can change it",
			})
			return false
		}
	}
	if kept, has := stored["console"]; has && kept != nil {
		params["console"] = kept
	} else {
		delete(params, "console")
	}
	return true
}

// changedDestination reports the first destination parameter the request
// sets to something other than what the file holds. References are
// resolved on both sides, so a form that shows the resolved address of a
// stored ${env:} is not mistaken for a change. A parameter the request
// leaves out is not a change.
func changedDestination(paths []string, stored, incoming map[string]interface{}) (string, bool) {
	for _, path := range paths {
		want := resolvedText(lookupPath(incoming, path))
		if want == "" {
			continue
		}
		if resolvedText(lookupPath(stored, path)) != want {
			return path, true
		}
	}
	return "", false
}

func resolvedText(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprint(v)
	}
	if resolved, err := configuration.SubstituteString(s); err == nil {
		return strings.TrimSpace(resolved)
	}
	return strings.TrimSpace(s)
}

// storedSecretsNotResent lists the secret parameters the file holds that
// the request does not carry itself.
func storedSecretsNotResent(secretPaths []string, stored, incoming map[string]interface{}) []string {
	var missing []string
	for _, path := range secretPaths {
		if isEmptyValue(lookupPath(stored, path)) || !isEmptyValue(lookupPath(incoming, path)) {
			continue
		}
		missing = append(missing, path)
	}
	return missing
}

func isEmptyValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case map[string]interface{}:
		return len(t) == 0
	case map[interface{}]interface{}:
		return len(t) == 0
	}
	return false
}

// credentialsStayWithDestination refuses a request that moves the
// destination of something the file holds credentials for without
// carrying those credentials itself. A credential stored for one address is
// never sent to another on the strength of a form that merely left it
// blank.
func credentialsStayWithDestination(params []spec.ParamSpec, secretPaths []string, stored, incoming map[string]interface{}) error {
	if stored == nil {
		return nil
	}
	moved, changed := changedDestination(spec.DestinationPaths(params), stored, incoming)
	if !changed {
		return nil
	}
	if missing := storedSecretsNotResent(secretPaths, stored, incoming); len(missing) > 0 {
		return fmt.Errorf("%s differs from the stored configuration, so the stored credentials (%s) are not used for it; send them with the request", moved, strings.Join(missing, ", "))
	}
	return nil
}

// withStoredCredentials returns the parameters to test with: the request
// completed with the secrets the file holds, unless the request moves the
// destination, in which case the credentials come from the request or the
// call fails.
func withStoredCredentials(params []spec.ParamSpec, secretPaths []string, stored, incoming map[string]interface{}) (map[string]interface{}, error) {
	if stored == nil {
		return incoming, nil
	}
	if err := credentialsStayWithDestination(params, secretPaths, stored, incoming); err != nil {
		return nil, err
	}
	if _, changed := changedDestination(spec.DestinationPaths(params), stored, incoming); changed {
		return incoming, nil
	}
	return configuration.KeepStoredValues(stored, incoming, secretPaths), nil
}
