package configuration

import (
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/data_store/outputspec"
	"senhub-agent.go/internal/agent/services/logredact"
)

// logIdentifierPattern adds, for the logs only, the identifier-style
// fields that are not secrets but should not reach shared log
// infrastructure either.
var logIdentifierPattern = regexp.MustCompile(`(?i)(user|login|email)`)

// redactSecretsInString is kept as a thin wrapper so existing callers and
// tests keep compiling; the implementation lives in logredact.
func redactSecretsInString(s string) string { return logredact.String(s) }

// SanitizeParamsForLog returns a deep copy of params with the value of any key
// matching the shared rule of logredact.IsSecretKey (plus the log-only
// identifier fields) replaced by "***". The original map is never
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
			"params": SanitizeOutputParamsForLog(s.Name, s.Params),
		})
	}
	return out
}

// SanitizeProbeParamsForLog is SanitizeParamsForLog plus the parameters
// the probe's spec declares secret, whatever their name.
func SanitizeProbeParamsForLog(probeType string, params map[string]interface{}) map[string]interface{} {
	out := SanitizeParamsForLog(params)
	if s, ok := spec.For(probeType); ok {
		for _, p := range s.SecretPaths() {
			maskSpecPath(out, strings.Split(p, "."))
		}
	}
	return out
}

// SanitizeOutputParamsForLog is SanitizeParamsForLog plus the parameters
// the output's spec declares secret, whatever their name.
func SanitizeOutputParamsForLog(outputType string, params map[string]interface{}) map[string]interface{} {
	out := SanitizeParamsForLog(params)
	if s, ok := outputspec.For(outputType); ok {
		for _, p := range s.SecretPaths() {
			maskSpecPath(out, strings.Split(p, "."))
		}
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
			"params": SanitizeProbeParamsForLog(p.Type, p.Params),
		})
	}
	return out
}

// sanitizeValueForLog masks v when key is sensitive, otherwise recurses into
// composite values. For slice elements the key is empty (an index carries no
// meaning) so each element is judged by its own inner keys.
func sanitizeValueForLog(key string, v interface{}) interface{} {
	if key != "" && (logredact.IsSecretKey(key) || logIdentifierPattern.MatchString(key)) {
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
	case string:
		return logredact.String(t)
	case []string:
		s := make([]string, len(t))
		for i, vv := range t {
			s[i] = logredact.String(vv)
		}
		return s
	default:
		return v
	}
}

// RedactSecretsInString masks the credentials a free-form message can
// carry (URI userinfo, URL query values, URL fragments, DSN passwords, HTTP
// authorization headers and standalone scheme credentials, generic key=value
// pairs, JWTs and cloud access key ids) before the text leaves the process, a
// remote health report for instance.
func RedactSecretsInString(s string) string { return logredact.String(s) }
