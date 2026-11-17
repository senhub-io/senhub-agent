package configuration

import (
	"fmt"
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/services/configuration/secret"
)

var fleetSecretRefPattern = regexp.MustCompile(`^\$\{secret:fleet\.([a-z0-9][a-z0-9._-]{0,63})\}$`)

// resolveSecretFunc is secret.Resolve; a variable so tests can stub the backend.
var resolveSecretFunc = secret.Resolve

// resolveManagedParams resolves only exact "${secret:fleet.<name>}" references in
// the top‑level string values of the given map. Any other string containing "${"
// (including inside lists or maps) is rejected.
func resolveManagedParams(in map[string]any) (ProbeConfigParams, error) {
	out := make(ProbeConfigParams, len(in))
	for k, v := range in {
		switch val := v.(type) {
		case string:
			if matches := fleetSecretRefPattern.FindStringSubmatch(val); matches != nil {
				name := matches[1]
				resolved, err := resolveSecretFunc("fleet."+name, "", false)
				if err != nil {
					return nil, fmt.Errorf("managed parameter %q: unresolved fleet secret: %w", k, err)
				}
				out[k] = resolved
			} else if strings.Contains(val, "${") {
				return nil, fmt.Errorf("managed parameter %q: only ${secret:fleet.<name>} references are allowed", k)
			} else {
				out[k] = val
			}
		default:
			if containsSecretRef(v) {
				return nil, fmt.Errorf("managed parameter %q: only ${secret:fleet.<name>} references are allowed", k)
			}
			out[k] = v
		}
	}
	return out, nil
}

// containsSecretRef recursively checks whether a value contains a string with "${".
func containsSecretRef(v any) bool {
	switch val := v.(type) {
	case string:
		return strings.Contains(val, "${")
	case []any:
		for _, elem := range val {
			if containsSecretRef(elem) {
				return true
			}
		}
	case map[string]any:
		for key, elem := range val {
			if strings.Contains(key, "${") || containsSecretRef(elem) {
				return true
			}
		}
	case []string:
		for _, s := range val {
			if strings.Contains(s, "${") {
				return true
			}
		}
	}
	return false
}
