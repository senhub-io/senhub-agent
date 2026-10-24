package agentstate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// LogRedactRule is one operator-defined masking rule: every match of
// Pattern (RE2) is rewritten with Replacement (Go regexp expansion).
type LogRedactRule struct {
	Name        string
	Pattern     string
	Replacement string
}

// LogRedactedCount is the lifetime number of occurrences a rule masked
// in the records of one probe.
type LogRedactedCount struct {
	Probe string
	Rule  string
	Count uint64
}

type redactRule struct {
	name        string
	re          *regexp.Regexp
	replacement string
	precheck    func(string) bool
}

type redactionRuleset struct {
	rules []redactRule
}

type redactionCounterKey struct {
	probe string
	rule  string
}

var (
	redactionConfig   atomic.Pointer[redactionRuleset]
	redactionCounters sync.Map
)

var builtinRedactRules = []redactRule{
	{
		name:        "credential_param",
		re:          regexp.MustCompile(`(?i)(["']?\b(?:password|passwd|pwd|passhash|apikey|api_key|token|access_token|secret)["']?\s*[=:]\s*["']?)([^\s&"',;]+)`),
		replacement: "${1}***",
		precheck:    credentialParamPrecheck,
	},
	{
		name:        "authorization_header",
		re:          regexp.MustCompile(`(?i)(\b(?:proxy-authorization|authorization)\b\s*["']?[=:]\s*["']?\s*)(?:(Digest\s+)([^\r\n]+)|((?:Bearer|Basic|Digest|Negotiate|NTLM)\s+)?([^\s"',;]+))`),
		replacement: "${1}${2}${4}***",
		precheck:    authorizationHeaderPrecheck,
	},
	{
		// PRTG writes its licence key in clear in Core.log and
		// CoreActivationLog.log on every core start:
		// `licensed for "<name>" (XXXXXX-XXXXXX-...)`, ten groups of six.
		name:        "prtg_license_key",
		re:          regexp.MustCompile(`\b[A-Z0-9]{6}(?:-[A-Z0-9]{6}){9}\b`),
		replacement: "***",
		precheck:    prtgLicenseKeyPrecheck,
	},
	{
		// The line after the licence: `SystemID: SYSTEMID-<5 groups of 8>`.
		name:        "prtg_system_id",
		re:          regexp.MustCompile(`\bSYSTEMID-[A-Z0-9]{8}(?:-[A-Z0-9]{8}){4}\b`),
		replacement: "SYSTEMID-***",
		precheck:    func(s string) bool { return strings.Contains(s, "SYSTEMID-") },
	},
}

// prtgLicenseKeyPrecheck skips the key pattern on lines that cannot hold
// one: the key alone carries nine dashes.
func prtgLicenseKeyPrecheck(s string) bool { return len(s) >= 69 && strings.Count(s, "-") >= 9 }

// builtinRedactRuleName reports whether name belongs to the preset, so a
// custom rule cannot take it.
func builtinRedactRuleName(name string) bool {
	for _, r := range builtinRedactRules {
		if r.name == name {
			return true
		}
	}
	return false
}

// ValidateLogRedactRules returns one error per rule that cannot be used,
// each naming the rule.
func ValidateLogRedactRules(rules []LogRedactRule) []error {
	_, errs := compileCustomRules(rules)
	return errs
}

// ConfigureLogRedaction installs the masking rules for every record
// published afterwards. Unusable rules are skipped and returned; the
// others still apply.
func ConfigureLogRedaction(preset bool, rules []LogRedactRule) []error {
	rs, errs := buildRedactionRuleset(preset, rules)
	redactionConfig.Store(rs)
	return errs
}

// LogRedactedCounts snapshots the monotonic masking counters, sorted by
// probe then rule.
func LogRedactedCounts() []LogRedactedCount {
	var out []LogRedactedCount
	redactionCounters.Range(func(k, v any) bool {
		key := k.(redactionCounterKey)
		out = append(out, LogRedactedCount{
			Probe: key.probe,
			Rule:  key.rule,
			Count: v.(*atomic.Uint64).Load(),
		})
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Probe != out[j].Probe {
			return out[i].Probe < out[j].Probe
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

func redactLogRecord(rec LogRecord) LogRecord {
	rs := loadRedactionRuleset()
	rec.Body = maskString(rs, rec.Body, rec.ProducerProbeName)
	rec.Attributes = maskStringMap(rs, rec.Attributes, rec.ProducerProbeName)
	rec.Fields = maskRecordFields(rs, rec.Fields, rec.ProducerProbeName)
	return rec
}

func resetLogRedactionForTest() {
	redactionConfig.Store(buildDefaultRedactionRuleset())
	var keys []redactionCounterKey
	redactionCounters.Range(func(k, _ any) bool {
		keys = append(keys, k.(redactionCounterKey))
		return true
	})
	for _, key := range keys {
		redactionCounters.Delete(key)
	}
}

func loadRedactionRuleset() *redactionRuleset {
	if rs := redactionConfig.Load(); rs != nil {
		return rs
	}
	redactionConfig.CompareAndSwap(nil, buildDefaultRedactionRuleset())
	return redactionConfig.Load()
}

func buildDefaultRedactionRuleset() *redactionRuleset {
	return &redactionRuleset{rules: append([]redactRule(nil), builtinRedactRules...)}
}

func buildRedactionRuleset(preset bool, rules []LogRedactRule) (*redactionRuleset, []error) {
	custom, errs := compileCustomRules(rules)
	rs := &redactionRuleset{}
	if preset {
		rs.rules = append(rs.rules, builtinRedactRules...)
	}
	rs.rules = append(rs.rules, custom...)
	return rs, errs
}

func compileCustomRules(rules []LogRedactRule) ([]redactRule, []error) {
	valid := make([]redactRule, 0, len(rules))
	var errs []error
	seen := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		if !validLogRedactRuleName(r.Name) {
			errs = append(errs, fmt.Errorf("log redaction rule %q: name must match ^[a-z0-9_]+$", r.Name))
			continue
		}
		if builtinRedactRuleName(r.Name) {
			errs = append(errs, fmt.Errorf("log redaction rule %q: reserved name", r.Name))
			continue
		}
		if _, ok := seen[r.Name]; ok {
			errs = append(errs, fmt.Errorf("log redaction rule %q: duplicate name", r.Name))
			continue
		}
		seen[r.Name] = struct{}{}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			errs = append(errs, fmt.Errorf("log redaction rule %q: pattern %q: %w", r.Name, r.Pattern, err))
			continue
		}
		valid = append(valid, redactRule{
			name:        r.Name,
			re:          re,
			replacement: r.Replacement,
		})
	}
	return valid, errs
}

func validLogRedactRuleName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func maskString(rs *redactionRuleset, s, probe string) string {
	if s == "" || rs == nil || len(rs.rules) == 0 {
		return s
	}
	out := s
	for i := range rs.rules {
		rule := &rs.rules[i]
		if rule.precheck != nil && !rule.precheck(out) {
			continue
		}
		out = applyRedactRule(rule, out, probe)
	}
	return out
}

func applyRedactRule(rule *redactRule, src, probe string) string {
	matches := rule.re.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return src
	}
	var b strings.Builder
	last := 0
	changed := false
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		matched := src[m[0]:m[1]]
		repl := string(rule.re.ExpandString(nil, rule.replacement, src, m))
		if repl == matched {
			continue
		}
		if !changed {
			b.Grow(len(src) + len(repl))
			changed = true
		}
		b.WriteString(src[last:m[0]])
		b.WriteString(repl)
		last = m[1]
		recordRedaction(probe, rule.name)
	}
	if !changed {
		return src
	}
	b.WriteString(src[last:])
	return b.String()
}

func maskStringMap(rs *redactionRuleset, attrs map[string]string, probe string) map[string]string {
	if len(attrs) == 0 {
		return attrs
	}
	var out map[string]string
	for k, v := range attrs {
		mv, keyed := maskByKey(rs, k, v, probe)
		if !keyed {
			mv = maskString(rs, v, probe)
		}
		if mv == v {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(attrs))
			for kk, vv := range attrs {
				out[kk] = vv
			}
		}
		out[k] = mv
	}
	if out == nil {
		return attrs
	}
	return out
}

func maskRecordFields(rs *redactionRuleset, fields map[string]any, probe string) map[string]any {
	if len(fields) == 0 {
		return fields
	}
	out, changed := maskFieldsMap(rs, fields, probe)
	if changed {
		return out
	}
	return fields
}

func maskAny(rs *redactionRuleset, v any, probe string) (any, bool) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return v, false
		}
		mv := maskString(rs, t, probe)
		if mv == t {
			return v, false
		}
		return mv, true
	case map[string]any:
		out, changed := maskFieldsMap(rs, t, probe)
		return out, changed
	case []any:
		out, changed := maskFieldsSlice(rs, t, probe)
		return out, changed
	default:
		return v, false
	}
}

func maskFieldsMap(rs *redactionRuleset, m map[string]any, probe string) (map[string]any, bool) {
	if len(m) == 0 {
		return m, false
	}
	var out map[string]any
	for k, child := range m {
		nv, changed := maskAny(rs, child, probe)
		if !changed {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(m))
			for kk, vv := range m {
				out[kk] = vv
			}
		}
		out[k] = nv
	}
	if out == nil {
		return m, false
	}
	return out, true
}

func maskFieldsSlice(rs *redactionRuleset, s []any, probe string) ([]any, bool) {
	if len(s) == 0 {
		return s, false
	}
	var out []any
	copied := false
	for i, child := range s {
		nv, changed := maskAny(rs, child, probe)
		if !changed {
			continue
		}
		if !copied {
			out = make([]any, len(s))
			copy(out, s)
			copied = true
		}
		out[i] = nv
	}
	if !copied {
		return s, false
	}
	return out, true
}

func recordRedaction(probe, rule string) {
	key := redactionCounterKey{probe: probe, rule: rule}
	val, _ := redactionCounters.LoadOrStore(key, new(atomic.Uint64))
	val.(*atomic.Uint64).Add(1)
}

func credentialParamPrecheck(s string) bool {
	return containsFold(s, "pass") ||
		containsFold(s, "pwd") ||
		containsFold(s, "apikey") ||
		containsFold(s, "api_key") ||
		containsFold(s, "token") ||
		containsFold(s, "secret")
}

func authorizationHeaderPrecheck(s string) bool {
	return containsFold(s, "authorization")
}

func containsFold(s, lowerKeyword string) bool {
	if lowerKeyword == "" {
		return true
	}
	if len(lowerKeyword) > len(s) {
		return false
	}
	for i := 0; i+len(lowerKeyword) <= len(s); i++ {
		if asciiLower(s[i]) != lowerKeyword[0] {
			continue
		}
		j := 1
		for j < len(lowerKeyword) && asciiLower(s[i+j]) == lowerKeyword[j] {
			j++
		}
		if j == len(lowerKeyword) {
			return true
		}
	}
	return false
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// ResetLogRedactionForTest restores the default masking configuration and
// clears the counters, for tests of packages that read them.
func ResetLogRedactionForTest() {
	resetLogRedactionForTest()
}

// credentialKeys are the attribute names whose whole value is a secret.
// A value under such a key carries no "key=" form for the patterns to
// find, so the preset masks it by its key: "password", "db.password",
// "http.request.header.authorization".
var credentialKeys = map[string]string{
	"password": "credential_param", "passwd": "credential_param", "pwd": "credential_param",
	"passhash": "credential_param", "apikey": "credential_param", "api_key": "credential_param",
	"token": "credential_param", "access_token": "credential_param", "secret": "credential_param",
	"authorization": "authorization_header", "proxy-authorization": "authorization_header",
}

// maskByKey masks the whole value of an attribute whose last name segment
// is a credential key, when the preset is on. An Authorization value keeps
// its scheme.
func maskByKey(rs *redactionRuleset, key, v, probe string) (string, bool) {
	if v == "" || !presetOn(rs) {
		return v, false
	}
	last := strings.ToLower(key)
	if i := strings.LastIndexByte(last, '.'); i >= 0 {
		last = last[i+1:]
	}
	rule, ok := credentialKeys[last]
	if !ok {
		return v, false
	}
	masked := "***"
	if rule == "authorization_header" {
		if sp := strings.IndexByte(strings.TrimSpace(v), ' '); sp > 0 {
			masked = strings.TrimSpace(v)[:sp+1] + "***"
		}
	}
	if masked == v {
		return v, true
	}
	recordRedaction(probe, rule)
	return masked, true
}

func presetOn(rs *redactionRuleset) bool {
	for i := range rs.rules {
		if rs.rules[i].name == "credential_param" {
			return true
		}
	}
	return false
}
