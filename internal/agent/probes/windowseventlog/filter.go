package windowseventlog

import (
	"path"
	"strings"
)

const redactedPlaceholder = "[REDACTED]"

// shouldEmit applies the operator-configured filters to a parsed event.
// All filters are AND-combined: an event must pass every configured
// filter to be emitted. Empty filters are no-ops (match everything).
//
// The wevtapi XPath query (see buildXPathQuery) already filters by
// level at the source for efficiency; this function is the
// authoritative second pass that also enforces the include/exclude
// EventID lists and source globs, and is the single point unit tests
// exercise without a Windows host.
func (c WindowsEventLogProbeConfig) shouldEmit(e parsedEvent) bool {
	if len(c.levelInts) > 0 && !containsInt(c.levelInts, e.Level) {
		return false
	}
	if len(c.IncludeEventIDs) > 0 && !containsInt(c.IncludeEventIDs, e.EventID) {
		return false
	}
	if containsInt(c.ExcludeEventIDs, e.EventID) {
		return false
	}
	if len(c.Sources) > 0 && !matchesAnyGlob(c.Sources, e.Provider) {
		return false
	}
	return true
}

func containsInt(haystack []int, needle int) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// matchesAnyGlob reports whether name matches any of the shell-style
// glob patterns (e.g. "Citrix*", "FSLogix*"). Matching is
// case-insensitive because Windows provider names are not
// case-sensitive in practice. A pattern with no metacharacters is an
// exact (case-insensitive) match.
func matchesAnyGlob(patterns []string, name string) bool {
	lname := strings.ToLower(name)
	for _, p := range patterns {
		lp := strings.ToLower(p)
		if ok, err := path.Match(lp, lname); err == nil && ok {
			return true
		}
	}
	return false
}

// sensitiveFieldKeys is the set of EventData field names redacted in
// PII mode. The Security channel logon events (4624/4625/4634/...) carry
// these. The list is conservative — over-redacting a field is safer
// than leaking it. See the GDPR note in the package doc.
var sensitiveFieldKeys = map[string]bool{
	"targetusername":    true,
	"subjectusername":   true,
	"targetdomainname":  true,
	"subjectdomainname": true,
	"ipaddress":         true,
	"workstationname":   true,
	"targetuserid":      true,
	"subjectuserid":     true,
	"targetsid":         true,
	"subjectsid":        true,
}

func isSensitiveField(key string) bool {
	return sensitiveFieldKeys[strings.ToLower(key)]
}

// redactSecurityBody blanks the human-readable body for Security-channel
// records in PII mode. The rendered Security message interpolates the
// sensitive EventData fields, so we cannot selectively scrub it; we
// replace it with a channel marker and rely on the (already
// field-redacted) attributes for non-PII context.
func redactSecurityBody(channel, body string) string {
	if strings.EqualFold(channel, "Security") {
		return "[REDACTED Security event body — PII mode enabled]"
	}
	return body
}
