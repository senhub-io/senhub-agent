package otlp

import (
	"os"
	"regexp"
)

// envVarRegexp matches the ${env:NAME} substitution syntax. NAME must
// be a valid POSIX env-var identifier ([A-Za-z_][A-Za-z0-9_]*), which
// also matches what the OTel collector accepts.
//
// We intentionally support the same syntax as the OTel collector so an
// operator who configures the collector with ${env:OTLP_BEARER_TOKEN}
// can use the same expression in the agent's storage block without
// learning a second convention.
var envVarRegexp = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces every ${env:VAR} occurrence in s with the value of
// the matching environment variable. Unset variables expand to the
// empty string — same behavior as the OTel collector. We never panic
// or surface an error for missing vars: the caller (typically a header
// value or an endpoint) gets to decide whether an empty result is
// acceptable.
func expandEnv(s string) string {
	if s == "" {
		return s
	}
	return envVarRegexp.ReplaceAllStringFunc(s, func(match string) string {
		// match is "${env:NAME}"; extract NAME.
		groups := envVarRegexp.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		return os.Getenv(groups[1])
	})
}

// expandEnvMap returns a copy of the map with every value passed
// through expandEnv. Original map is left untouched.
func expandEnvMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = expandEnv(v)
	}
	return out
}
