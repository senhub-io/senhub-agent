package http

import (
	"regexp"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
)

// guessField names the parameter an error message most probably
// concerns, so the console can put the message under the right field.
// The message is matched against the probe's own parameter paths first
// (a parser that says "tls.ca_file: no such file" names its key), then
// against what common failures mention: credentials, the host, the
// port. Nothing certain, nothing claimed: the caller keeps the message
// and only adds the hint.
func guessField(probeType, message string) string {
	msg := strings.ToLower(message)
	if msg == "" {
		return ""
	}
	ps, has := spec.For(probeType)
	if !has {
		return ""
	}
	return guessFieldIn(ps.DeclaredKeys(), msg)
}

// guessFieldIn is guessField against an explicit set of dotted paths.
func guessFieldIn(declared map[string]struct{}, message string) string {
	msg := strings.ToLower(message)
	if msg == "" {
		return ""
	}
	paths := make([]string, 0, len(declared))
	for p := range declared {
		paths = append(paths, p)
	}
	// Longest path first, so tls.ca_file wins over tls when both match.
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) > len(paths[j])
		}
		return paths[i] < paths[j]
	})
	for _, p := range paths {
		if wordIn(msg, strings.ToLower(p)) {
			return p
		}
	}
	for _, rule := range fieldHintRules {
		if !rule.pattern.MatchString(msg) {
			continue
		}
		for _, candidate := range rule.fields {
			for _, p := range paths {
				if p == candidate {
					return p
				}
			}
		}
	}
	return ""
}

var fieldHintRules = []struct {
	pattern *regexp.Regexp
	fields  []string
}{
	{regexp.MustCompile(`access denied|authentication failed|password|auth(entication)? (failed|error)|401|403|unauthori[sz]ed|login failed|bad credentials|invalid credentials|wrong community|no such user|usm`), []string{"password", "username", "community", "bearer_token", "v3", "auth"}},
	{regexp.MustCompile(`no such host|name resolution|lookup .* no such|dns|unknown host|could not resolve`), []string{"host", "target", "targets", "endpoint", "server", "url"}},
	{regexp.MustCompile(`connection refused|timed? ?out|no route to host|unreachable|i/o timeout|deadline exceeded|dial tcp|dial udp|request timeout|did not answer|no response`), []string{"host", "target", "targets", "endpoint", "port", "url"}},
	{regexp.MustCompile(`x509|certificate|tls handshake|ssl`), []string{"tls", "insecure_skip_verify", "ca_file"}},
	{regexp.MustCompile(`no such file|permission denied|not found`), []string{"paths", "path", "command", "socket_path", "bookmark_path"}},
}

// wordIn reports whether needle appears in msg as a whole token, so
// "port" does not match "report" and "host" does not match "ghost".
func wordIn(msg, needle string) bool {
	idx := 0
	for {
		i := strings.Index(msg[idx:], needle)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(needle)
		before := start == 0 || !isWordByte(msg[start-1])
		after := end == len(msg) || !isWordByte(msg[end])
		if before && after {
			return true
		}
		idx = start + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
