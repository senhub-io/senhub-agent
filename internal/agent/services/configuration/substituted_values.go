package configuration

import (
	"sort"
	"strings"
	"sync"
)

// minMaskedLen is the shortest substituted value the log sanitizers mask.
// Masking a one- or two-character value ("1", "on") would rewrite unrelated
// text in every log line.
const minMaskedLen = 4

// substituted holds the values ${env:}, ${file:} and ${secret:} references
// resolved to during the last configuration loads. The agent knows where a
// value came from, so the log sanitizers mask it whatever its key.
//
// Two generations are kept: the load in force and the one before it. A
// reload logs the old and the new probes side by side, and a rotated secret
// must not reappear in clear in the "old" half. Each load replaces the
// older generation, so memory stays bounded by two configurations.
//
// A value that came from the `:-default` part of a reference is not
// recorded: a default is written in the file, it is not a secret.
var substituted = &substitutedValues{}

type substitutedValues struct {
	mu   sync.RWMutex
	cur  map[string]struct{}
	prev map[string]struct{}
	// ordered is the union of both generations, longest first, so a value
	// that contains another is masked whole.
	ordered []string
}

// recorder collects the substituted values of one load.
type recorder struct{ values map[string]struct{} }

func (r *recorder) add(v string) {
	if r == nil || len(v) < minMaskedLen {
		return
	}
	if r.values == nil {
		r.values = map[string]struct{}{}
	}
	r.values[v] = struct{}{}
}

func (r *recorder) list() []string {
	out := make([]string, 0, len(r.values))
	for v := range r.values {
		out = append(out, v)
	}
	return out
}

func (s *substitutedValues) rotate(next map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prev, s.cur = s.cur, next
	s.rebuild()
}

func (s *substitutedValues) rebuild() {
	union := make(map[string]struct{}, len(s.cur)+len(s.prev))
	for v := range s.cur {
		union[v] = struct{}{}
	}
	for v := range s.prev {
		union[v] = struct{}{}
	}
	s.ordered = s.ordered[:0]
	for v := range union {
		s.ordered = append(s.ordered, v)
	}
	sort.Slice(s.ordered, func(i, j int) bool {
		if len(s.ordered[i]) != len(s.ordered[j]) {
			return len(s.ordered[i]) > len(s.ordered[j])
		}
		return s.ordered[i] < s.ordered[j]
	})
}

func (s *substitutedValues) mask(in string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.ordered {
		if strings.Contains(in, v) {
			in = strings.ReplaceAll(in, v, "***")
		}
	}
	return in
}

// RecordSubstituted registers values resolved outside the loader (a managed
// overlay resolving ${secret:fleet.*} itself, for instance) so the log
// sanitizers mask them. The values live in the current generation and are
// dropped with it at the second following load.
func RecordSubstituted(values ...string) {
	substituted.mu.Lock()
	defer substituted.mu.Unlock()
	if substituted.cur == nil {
		substituted.cur = map[string]struct{}{}
	}
	for _, v := range values {
		if len(v) >= minMaskedLen {
			substituted.cur[v] = struct{}{}
		}
	}
	substituted.rebuild()
}

func maskSubstituted(in string) string { return substituted.mask(in) }
