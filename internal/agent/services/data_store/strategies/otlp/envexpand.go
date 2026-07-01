package otlp

import (
	"senhub-agent.go/internal/agent/services/configuration"
)

// expandEnv resolves ${env:}, ${file:} and ${secret:} references in s through
// the SHARED configuration resolver, so an OTLP endpoint or header value gets
// the same substitution — including the OS-native secret backend and the
// `:-default` form — as the rest of the config. Previously this path had its own
// env-only expander, which meant an OTLP bearer token could not be file- or
// secret-backed.
//
// On a resolution error (a missing ${file:}/${secret:} with no default) the
// original string is returned unchanged so the failure surfaces downstream
// (e.g. the backend rejects an unresolved token) rather than being silently
// turned into an empty header.
func expandEnv(s string) string {
	if s == "" {
		return s
	}
	out, err := configuration.SubstituteString(s)
	if err != nil {
		return s
	}
	return out
}

// expandEnvMap returns a copy of the map with every value passed through
// expandEnv. The original map is left untouched.
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
