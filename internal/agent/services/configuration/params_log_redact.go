package configuration

import (
	"regexp"
	"strings"
)

// logSensitiveKeyPattern is a superset of the show-redact pattern.
// `show.go`'s secretFieldPattern protects file-backed and named-secret
// values when an operator prints the config. The log path needs the
// same coverage PLUS the identifier-style fields (`user`, `login`,
// `username`, `email`) that aren't secrets per se but should not be
// echoed into shared log infrastructure either.
//
// Case-insensitive substring match: catches `api_key`, `auth_token`,
// `client_secret`, `db_password`, `pub400_user`, `auth_login`,
// `Authorization` (the OTLP strategy's `headers.Authorization: Bearer <token>`)…
// The case-insensitivity is necessary because YAML keys are written
// in mixed conventions (snake_case, camelCase) across the probe set.
//
// `authorization`/`bearer` are included so a resolved bearer token carried under
// an `Authorization` header key is masked before it reaches any log sink — the
// token is otherwise logged in cleartext when a strategy fails to start, at
// Error level, i.e. exactly during a misconfiguration incident. They are the
// full words (not a bare `auth`) so a legitimate `auth:` container key — e.g.
// citrix `director.auth.password` — still recurses and masks only the leaf
// secret rather than collapsing the whole subtree.
var logSensitiveKeyPattern = regexp.MustCompile(`(?i)(key|token|password|passphrase|secret|user|login|email|credential|community|authorization|bearer|license|jwt)`)

// uriUserinfoPattern masks the whole userinfo of scheme://...@host.
var uriUserinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/?#\s]*@`)

// urlQueryPattern matches a URL-like token followed by a query string.
var urlQueryPattern = regexp.MustCompile(`((?:[a-zA-Z][a-zA-Z0-9+.\-]*://[^?\s#]*|/[^?\s#]*)\?)([^#\s]*)`)

// queryParamPattern matches one name=value pair inside a query string.
var queryParamPattern = regexp.MustCompile(`([^?&#=\s]+)=([^&#\s]*)`)

// dsnPasswordPattern matches password=..., pwd=... and passwd=... pairs in
// key=value connection strings (libpq, ODBC, query strings), quoted or not.
var dsnPasswordPattern = regexp.MustCompile(`(?i)(password|passwd|pwd)(\s*=\s*)('[^']*'|"[^"]*"|[^\s;&'"]+)`)

// authorizationHeaderPattern matches an Authorization header with any scheme
// (`Authorization: Digest <cred>`, `Authorization=Token <cred>`) or with a
// single raw credential and no scheme word (`Authorization: <cred>`).
var authorizationHeaderPattern = regexp.MustCompile(`(?i)\bAuthorization\s*[:=]\s*(?:([A-Za-z][A-Za-z0-9._~+-]*)\s+([^\s]+)|([^\s]+))`)

// httpAuthSchemePattern matches a standalone HTTP authorization scheme followed
// by its credential; the credential is masked only when it looks like one.
var httpAuthSchemePattern = regexp.MustCompile(`(?i)\b(Bearer|Basic)(\s+)([^\s]+)`)

// genericCredentialPairPattern matches key=value / key: value pairs; the key is
// judged separately by isSensitivePairKey so `author`, `authority` and
// `authorized` stay untouched.
var genericCredentialPairPattern = regexp.MustCompile(`([^\s=:;&'"]+)(\s*[=:]\s*)('[^']*'|"[^"]*"|[^\s;&'"#]+)`)

// jwtPattern matches a JSON Web Token.
var jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)

// cloudAccessKeyIDPattern matches a cloud access key id at a word boundary.
var cloudAccessKeyIDPattern = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)

// sensitivePairKeyNames are the key names that make a key=value / key: value
// pair sensitive when they appear as a whole segment (split on `_`, `-`, `.`),
// as the whole key, or as a suffix of the key.
var sensitivePairKeyNames = []string{
	"token", "secret", "apikey", "api_key", "api-key", "passphrase",
	"credential", "auth", "password", "passwd", "pwd",
}

// isSensitivePairKey reports whether key names a sensitive pair. Matching is
// case-insensitive and uses whole segments, the whole key, or a suffix, so
// `client_secret`, `x-api-key` and `--api-key` match while `author`,
// `authority` and `authorized` do not.
func isSensitivePairKey(key string) bool {
	k := strings.ToLower(key)
	segments := strings.FieldsFunc(k, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for _, name := range sensitivePairKeyNames {
		if k == name || strings.HasSuffix(k, name) {
			return true
		}
		for _, seg := range segments {
			if seg == name {
				return true
			}
		}
	}
	return false
}

// looksLikeCredential reports whether s is long enough and contains a digit or
// one of the URL-safe/base64-ish punctuation characters that real credentials
// usually carry. Short prose after Bearer/Basic is left alone.
func looksLikeCredential(s string) bool {
	if len(s) < 8 {
		return false
	}
	return strings.ContainsAny(s, "0123456789._~+/=-")
}

// maskURLQuery masks every value in the query part of a URL-like token.
func maskURLQuery(s string) string {
	parts := urlQueryPattern.FindStringSubmatch(s)
	if parts == nil {
		return s
	}
	return parts[1] + queryParamPattern.ReplaceAllString(parts[2], "${1}=***")
}

// redactSecretsInString masks credentials embedded in a free-form value
// whatever its key: URI userinfo, URL query values, DSN passwords, HTTP
// authorization headers and standalone scheme credentials, generic key=value
// pairs, JWTs and cloud access key ids.
func redactSecretsInString(s string) string {
	s = uriUserinfoPattern.ReplaceAllString(s, "${1}***@")
	s = urlQueryPattern.ReplaceAllStringFunc(s, maskURLQuery)
	s = dsnPasswordPattern.ReplaceAllString(s, "${1}${2}***")
	s = authorizationHeaderPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := authorizationHeaderPattern.FindStringSubmatch(m)
		if len(parts) != 4 {
			return m
		}
		if parts[1] != "" {
			return "Authorization: " + parts[1] + " ***"
		}
		return "Authorization: ***"
	})
	s = httpAuthSchemePattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := httpAuthSchemePattern.FindStringSubmatch(m)
		if len(parts) != 4 || !looksLikeCredential(parts[3]) {
			return m
		}
		return parts[1] + parts[2] + "***"
	})
	s = genericCredentialPairPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := genericCredentialPairPattern.FindStringSubmatch(m)
		if len(parts) != 4 || !isSensitivePairKey(parts[1]) {
			return m
		}
		return parts[1] + parts[2] + "***"
	})
	s = jwtPattern.ReplaceAllString(s, "***")
	s = cloudAccessKeyIDPattern.ReplaceAllString(s, "***")
	return s
}

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
	case string:
		return redactSecretsInString(t)
	case []string:
		s := make([]string, len(t))
		for i, vv := range t {
			s[i] = redactSecretsInString(vv)
		}
		return s
	default:
		return v
	}
}

// RedactSecretsInString masks the credentials a free-form message can
// carry (URI userinfo, URL query values, DSN passwords, HTTP authorization
// headers and standalone scheme credentials, generic key=value pairs, JWTs,
// cloud access key ids) before the text leaves the process, a remote health
// report for instance.
func RedactSecretsInString(s string) string { return redactSecretsInString(s) }
