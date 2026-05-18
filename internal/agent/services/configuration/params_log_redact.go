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
var logSensitiveKeyPattern = regexp.MustCompile(`(?i)(key|token|password|secret|user|login|email|credential)`)

// SanitizeParamsForLog returns a shallow copy of params with any key
// matching logSensitiveKeyPattern replaced by "***". The original map
// is never mutated — the caller's runtime config stays intact, only
// the log-bound view is masked.
//
// Nested maps are not recursed into today: probe params are flat by
// convention in our schema. Add recursion only when a real probe
// emerges that nests credentials.
func SanitizeParamsForLog(params map[string]interface{}) map[string]interface{} {
	if params == nil {
		return nil
	}
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		if logSensitiveKeyPattern.MatchString(k) {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}
