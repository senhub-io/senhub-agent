package configuration

import "regexp"

// logSensitiveKeyPattern is a superset of the show-redact pattern.
// `show.go`'s secretFieldPattern protects file-backed and named-secret
// values when an operator prints the config. The log path needs the
// same coverage PLUS the identifier-style fields (`user`, `login`,
// `username`, `email`) that aren't secrets per se but should not be
// echoed into shared log infrastructure either.
//
// Case-insensitive substring match: catches `api_key`, `auth_token`,
// `client_secret`, `db_password`, `pub400_user`, `auth_login` …
// The case-insensitivity is necessary because YAML keys are written
// in mixed conventions (snake_case, camelCase) across the probe set.
var logSensitiveKeyPattern = regexp.MustCompile(`(?i)(key|token|password|passphrase|secret|user|login|email|credential|community)`)

// SanitizeParamsForLog returns a deep copy of params with the value of any key
// matching logSensitiveKeyPattern replaced by "***". The original map is never
// mutated — the caller's runtime config stays intact, only the log-bound view
// is masked.
//
// It RECURSES into nested maps and slices, so credentials that live below the
// top level — citrix `director.auth.password`, snmp `v3.users[].auth_password` —
// are masked too. A sensitive key masks its entire value, even a nested object.
func SanitizeParamsForLog(params map[string]interface{}) map[string]interface{} {
	if params == nil {
		return nil
	}
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		out[k] = sanitizeValueForLog(k, v)
	}
	return out
}

// SanitizeStorageForLog returns a log-safe view of a storage/strategies list:
// each entry keeps its name but its Params map is passed through
// SanitizeParamsForLog so resolved credentials (DSNs, bearer tokens, bind
// secrets) never reach shared log infrastructure. The input slice and its maps
// are never mutated.
func SanitizeStorageForLog(list []StorageConfig) []map[string]interface{} {
	if list == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]interface{}{
			"name":   s.Name,
			"params": SanitizeParamsForLog(s.Params),
		})
	}
	return out
}

// SanitizeProbesForLog returns a log-safe view of a probes list. Same contract
// as SanitizeStorageForLog: the Params map of each probe is masked so probe
// credentials (db passwords, SNMP communities, API keys) are not echoed.
func SanitizeProbesForLog(list []ProbeConfig) []map[string]interface{} {
	if list == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]interface{}{
			"name":   p.Name,
			"type":   p.Type,
			"params": SanitizeParamsForLog(p.Params),
		})
	}
	return out
}

// sanitizeValueForLog masks v when key is sensitive, otherwise recurses into
// composite values. For slice elements the key is empty (an index carries no
// meaning) so each element is judged by its own inner keys.
func sanitizeValueForLog(key string, v interface{}) interface{} {
	if key != "" && logSensitiveKeyPattern.MatchString(key) {
		return "***"
	}
	switch t := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, vv := range t {
			m[k] = sanitizeValueForLog(k, vv)
		}
		return m
	case map[interface{}]interface{}:
		m := make(map[interface{}]interface{}, len(t))
		for k, vv := range t {
			ks, _ := k.(string)
			m[k] = sanitizeValueForLog(ks, vv)
		}
		return m
	case []interface{}:
		s := make([]interface{}, len(t))
		for i, vv := range t {
			s[i] = sanitizeValueForLog("", vv)
		}
		return s
	default:
		return v
	}
}
