package agentstate

// The two parameter rules are the hot path of the log rail: they run on
// every line, and an unanchored case-insensitive regexp costs tens of
// microseconds per 100 bytes. These scanners find the same matches in one
// linear pass with no backtracking.

var credentialParamKeys = []string{
	"password", "passwd", "pwd", "passhash", "apikey", "api_key", "token",
	"access_token", "refresh_token", "id_token", "client_secret", "secret",
}

var urlQuerySecretKeys = []string{
	"key", "access_key", "auth_key", "sig", "signature", "x-api-key", "apikey",
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isRE2Space(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// keysAt reports whether some key of keys matches s at i, ignoring ASCII
// case, and fn accepts the text that follows it. Keys are tried in order,
// the way the alternation of a regexp backtracks.
func keysAt(s string, i int, keys []string, fn func(end int) bool) bool {
	for _, k := range keys {
		if i+len(k) > len(s) {
			continue
		}
		j := 0
		for j < len(k) && asciiLower(s[i+j]) == k[j] {
			j++
		}
		if j == len(k) && fn(i+len(k)) {
			return true
		}
	}
	return false
}

// credentialValueSpan reads `["']?\s*[=:]\s*["']?VALUE` from pos and
// returns the value bounds; VALUE is a run of bytes outside whitespace and
// & " ' , ;
func credentialValueSpan(s string, pos int) (int, int, bool) {
	if pos < len(s) && (s[pos] == '"' || s[pos] == '\'') {
		pos++
	}
	for pos < len(s) && isRE2Space(s[pos]) {
		pos++
	}
	if pos >= len(s) || (s[pos] != '=' && s[pos] != ':') {
		return 0, 0, false
	}
	pos++
	for pos < len(s) && isRE2Space(s[pos]) {
		pos++
	}
	if pos < len(s) && (s[pos] == '"' || s[pos] == '\'') {
		pos++
	}
	start := pos
	for pos < len(s) {
		c := s[pos]
		if isRE2Space(c) || c == '&' || c == '"' || c == '\'' || c == ',' || c == ';' {
			break
		}
		pos++
	}
	if pos == start {
		return 0, 0, false
	}
	return start, pos, true
}

func scanCredentialParam(s string) (string, int) {
	var out []byte
	last, count := 0, 0
	for i := 0; i < len(s); {
		c := asciiLower(s[i])
		if !isWordByte(s[i]) || (i > 0 && isWordByte(s[i-1])) || !credentialFirstByte(c) {
			i++
			continue
		}
		var vs, ve int
		if !keysAt(s, i, credentialParamKeys, func(end int) bool {
			var ok bool
			vs, ve, ok = credentialValueSpan(s, end)
			return ok
		}) {
			i++
			continue
		}
		if s[vs:ve] == "***" {
			i = ve
			continue
		}
		if out == nil {
			out = make([]byte, 0, len(s)+8)
		}
		out = append(out, s[last:vs]...)
		out = append(out, "***"...)
		last, i = ve, ve
		count++
	}
	if count == 0 {
		return s, 0
	}
	out = append(out, s[last:]...)
	return string(out), count
}

func credentialFirstByte(c byte) bool {
	switch c {
	case 'p', 'a', 't', 'r', 'i', 'c', 's':
		return true
	}
	return false
}

func scanURLQuerySecret(s string) (string, int) {
	var out []byte
	last, count := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] != '?' && s[i] != '&' {
			continue
		}
		var vs, ve int
		if !keysAt(s, i+1, urlQuerySecretKeys, func(end int) bool {
			if end >= len(s) || s[end] != '=' {
				return false
			}
			vs, ve = end+1, end+1
			for ve < len(s) {
				c := s[ve]
				if isRE2Space(c) || c == '&' || c == '#' || c == '"' || c == '\'' || c == ',' || c == ';' || c == '<' || c == '>' {
					break
				}
				ve++
			}
			return ve > vs
		}) {
			continue
		}
		if s[vs:ve] == "***" {
			i = ve - 1
			continue
		}
		if out == nil {
			out = make([]byte, 0, len(s)+8)
		}
		out = append(out, s[last:vs]...)
		out = append(out, "***"...)
		last, i = ve, ve-1
		count++
	}
	if count == 0 {
		return s, 0
	}
	out = append(out, s[last:]...)
	return string(out), count
}
