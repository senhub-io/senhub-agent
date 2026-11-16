package logredact

import (
	"regexp"
	"strings"
)

// secretKeyWords is the one list of words that make a parameter or
// field name secret, for the configuration views, the logs, the
// console and free text alike.
var secretKeyWords = []string{"pass", "pwd", "secret", "token", "key", "session", "private", "cookie", "auth", "credential", "dsn", "uri", "community", "bearer", "license", "jwt"}

var secretKeyPattern = regexp.MustCompile(`(?i)(` + strings.Join(secretKeyWords, "|") + `)`)

// IsSecretKey reports whether a structured key (a YAML parameter, a
// header, a log field) names a secret: any of secretKeyWords as a
// substring, case ignored.
func IsSecretKey(name string) bool { return secretKeyPattern.MatchString(name) }

// uriUserinfoPattern masks the whole userinfo of scheme://...@host.
var uriUserinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/?#\s]*@`)

// urlQueryPattern matches a URL-like token followed by a query string.
var urlQueryPattern = regexp.MustCompile(`((?:[a-zA-Z][a-zA-Z0-9+.\-]*://[^?\s#]*|/[^?\s#]*)\?)([^#\s]*)`)

// urlFragmentPattern masks the fragment of a URL-like token only; a '#' in
// plain text is left alone.
var urlFragmentPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^\s#]*)(#[^\s]*)`)

// queryParamPattern matches one name=value pair inside a query string.
var queryParamPattern = regexp.MustCompile(`([^?&#=\s]+)=([^&#\s]*)`)

// dsnPasswordPattern matches password=..., pwd=... and passwd=... pairs in
// key=value connection strings (libpq, ODBC, query strings), quoted or not.
var dsnPasswordPattern = regexp.MustCompile(`(?i)(password|passwd|pwd)(\s*=\s*)('[^']*'|"[^"]*"|[^\s;&'"]+)`)

// authorizationHeaderPattern matches an Authorization header and captures the
// header prefix plus everything after the separator. The credential part is
// parsed separately so only the credential is masked, never the rest of the line.
var authorizationHeaderPattern = regexp.MustCompile(`(?i)\b(Authorization\s*[:=]\s*)(.*)`)

// authParamListPattern matches a comma-separated list of key=value parameters at
// the start of a Digest-style credential. A parameter value is either quoted or
// an unquoted run without spaces or commas.
var authParamListPattern = regexp.MustCompile(`^(?:[^=\s,]+=(?:\"[^\"]*\"|'[^']*'|[^,\s]+))(?:,\s*[^=\s,]+=(?:\"[^\"]*\"|'[^']*'|[^,\s]+))*`)

// authTokenPattern matches a single non-space credential token.
var authTokenPattern = regexp.MustCompile(`^\S+`)

// allLettersPattern recognizes a lone alphabetic word, i.e. a scheme with no credential.
var allLettersPattern = regexp.MustCompile(`^[A-Za-z]+$`)

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
// pair sensitive when they appear as the whole key, as a whole segment
// (split on `_`, `-`, `.`), or as a plain suffix of the key. It uses the
// same word list as IsSecretKey, plus the explicit spellings that carry no
// shared keyword.
var sensitivePairKeyNames = append(append([]string{}, secretKeyWords...), "apikey", "api_key", "api-key", "passphrase", "password", "passwd")

// isSensitivePairKey reports whether key names a sensitive pair. Matching is
// case-insensitive and uses the whole key, whole segments, or a plain suffix,
// so `oauth`, `authtoken`, `accessToken`, `client_secret`, `x-api-key` and
// `--api-key` match while `author`, `authority` and `authorized` do not.
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
	return parts[1] + queryParamPattern.ReplaceAllStringFunc(parts[2], maskQueryValue)
}

// maskQueryValue masks one name=value pair whole. The value runs to the
// next blank, so prose punctuation glued to it ("?x=1: dial") goes with it:
// a value may end with that same character, and the last character of a
// secret must never stay visible.
func maskQueryValue(m string) string {
	parts := queryParamPattern.FindStringSubmatch(m)
	if len(parts) != 3 {
		return m
	}
	return parts[1] + "=***"
}

// maskURLFragment masks a URL fragment whole, up to the next blank, prose
// punctuation included: the last character of a secret must never stay
// visible. A bare "#" carries nothing and is left as it is.
func maskURLFragment(m string) string {
	parts := urlFragmentPattern.FindStringSubmatch(m)
	if len(parts) != 3 {
		return m
	}
	if len(parts[2]) == 1 {
		return m
	}
	return parts[1] + "#***"
}

// maskAuthorizationCredential masks only the credential portion of an
// Authorization header value: a Digest-style parameter list is masked from the
// first parameter through the last parameter of that list, otherwise a single
// non-space token is masked. Text after the credential is preserved.
func maskAuthorizationCredential(rest string) string {
	if rest == "" {
		return ""
	}
	if m := authParamListPattern.FindString(rest); m != "" {
		return "***" + rest[len(m):]
	}
	if m := authTokenPattern.FindString(rest); m != "" {
		return "***" + rest[len(m):]
	}
	return "***"
}

// Rule names reported to the counter, one per masked form.
const (
	RuleURLUserinfo         = "url_userinfo"
	RuleURLQuery            = "url_query"
	RuleURLFragment         = "url_fragment"
	RuleDSNPassword         = "dsn_password"
	RuleAuthorizationHeader = "authorization_header"
	RuleBearerToken         = "bearer_token"
	RuleCredentialParam     = "credential_param"
	RuleJWT                 = "jwt"
	RuleAWSAccessKeyID      = "aws_access_key_id"
)

// String masks credentials embedded in a free-form value whatever its key:
// URI userinfo, URL query values, URL fragments, DSN passwords, HTTP
// authorization headers and standalone scheme credentials, generic key=value
// pairs, JWTs and cloud access key ids.
func String(s string) string {
	return Redact(s, nil)
}

// Redact is String with an observer: count, when not nil, is called once
// per masked occurrence with the rule name. A replacement that leaves the
// text unchanged, as on text masked already, is not counted.
func Redact(s string, count func(rule string)) string {
	counted := func(rule string, re *regexp.Regexp, repl func(string) string) {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			out := repl(m)
			if out != m && count != nil {
				count(rule)
			}
			return out
		})
	}
	counted(RuleURLUserinfo, uriUserinfoPattern, func(m string) string {
		return uriUserinfoPattern.ReplaceAllString(m, "${1}***@")
	})
	counted(RuleURLQuery, urlQueryPattern, maskURLQuery)
	counted(RuleURLFragment, urlFragmentPattern, maskURLFragment)
	counted(RuleDSNPassword, dsnPasswordPattern, func(m string) string {
		return dsnPasswordPattern.ReplaceAllString(m, "${1}${2}***")
	})
	counted(RuleAuthorizationHeader, authorizationHeaderPattern, func(m string) string {
		parts := authorizationHeaderPattern.FindStringSubmatch(m)
		if len(parts) != 3 {
			return m
		}
		rest := parts[2]
		if rest == "" {
			return m
		}
		// A lone alphabetic word is a scheme with no credential: leave it as is.
		if !strings.ContainsAny(rest, " \t") && allLettersPattern.MatchString(rest) {
			return m
		}
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			scheme := rest[:i]
			cred := strings.TrimLeft(rest[i:], " \t")
			return parts[1] + scheme + " " + maskAuthorizationCredential(cred)
		}
		return parts[1] + maskAuthorizationCredential(rest)
	})
	counted(RuleBearerToken, httpAuthSchemePattern, func(m string) string {
		parts := httpAuthSchemePattern.FindStringSubmatch(m)
		if len(parts) != 4 || !looksLikeCredential(parts[3]) {
			return m
		}
		return parts[1] + parts[2] + "***"
	})
	counted(RuleCredentialParam, genericCredentialPairPattern, func(m string) string {
		parts := genericCredentialPairPattern.FindStringSubmatch(m)
		if len(parts) != 4 || !isSensitivePairKey(parts[1]) {
			return m
		}
		return parts[1] + parts[2] + "***"
	})
	counted(RuleJWT, jwtPattern, func(string) string { return "***" })
	counted(RuleAWSAccessKeyID, cloudAccessKeyIDPattern, func(string) string { return "***" })
	return s
}
