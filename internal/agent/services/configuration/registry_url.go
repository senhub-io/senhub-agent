package configuration

import (
	"net/url"
	"strings"
	"sync"
)

// The registry URL an operator configures is a BASE. The agent appends
// the rest itself, and it appends more than one thing:
//
//	/releases/releases.json          the stable version list
//	/releases/beta/releases.json     the beta version list
//	/download/<version>/metadata.json
//	/download/<version>/<artifact>
//
// Two different first segments — /releases for the index, /download for
// the artifacts — is what makes the base unguessable. An operator who
// has only ever seen a download link reasonably writes the /releases or
// /download part into the base, and every derived URL then doubles it.
//
// The agent used to strip exactly one shape, a trailing "/releases"
// (#747), on exactly one code path, and warn about it on a call made
// once per datapoint batch. This normalises every shape anyone has
// plausibly written, on the single path every caller goes through, and
// says so once (#840).

// registrySuffixes are the paths a configured base may already carry.
// Longest first: "/releases/beta/releases.json" must be tried before
// "/releases", or the first match would leave "/beta/releases.json"
// behind.
var registrySuffixes = []string{
	"/releases/beta/releases.json",
	"/releases/releases.json",
	"/releases/beta",
	"/releases",
	"/download",
}

// NormalizeRegistryURL reduces a configured registry URL to the base the
// agent can append to, and reports whether it had to change anything.
//
// It is deliberately conservative: it removes only the segments the
// agent itself appends. A base pointing at a subdirectory the operator
// genuinely serves from — a mirror under /artifacts, say — is left
// alone, because the agent cannot tell that apart from an intended path
// and guessing would break a working configuration to fix a broken one.
//
// An input that is not a URL at all is returned untouched: reporting
// that is the validator's job, not this function's.
func NormalizeRegistryURL(raw string) (normalized string, changed bool) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return raw, false
	}

	// Strip repeatedly: ".../releases/releases" is a shape a previous
	// half-fix could have left behind, and one pass would only take the
	// last segment.
	for {
		before := trimmed
		for _, suffix := range registrySuffixes {
			if strings.HasSuffix(trimmed, suffix) {
				trimmed = strings.TrimSuffix(trimmed, suffix)
				trimmed = strings.TrimRight(trimmed, "/")
				break
			}
		}
		if trimmed == before {
			break
		}
	}

	// Everything was a suffix: the operator configured the index URL and
	// nothing else. There is no base left to append to, so keep the
	// original and let the validator say what is wrong.
	if trimmed == "" || isSchemeOnly(trimmed) {
		return raw, false
	}

	if trimmed == strings.TrimRight(strings.TrimSpace(raw), "/") && trimmed == raw {
		return raw, false
	}
	return trimmed, trimmed != raw
}

// isSchemeOnly reports whether stripping left nothing but "https:" or
// "https://host"-less remnants.
func isSchemeOnly(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return true
	}
	return u.Host == ""
}

// RegistryURLProblem describes a configured registry URL the agent
// cannot use, for `agent config check` to report before the value ever
// silently breaks updates in the field.
type RegistryURLProblem struct {
	// Reason is operator-facing and says what to write instead.
	Reason string
	// Suggestion is the value the agent would use, when it can derive
	// one; empty when it cannot.
	Suggestion string
}

// CheckRegistryURL validates a configured registry URL. It returns nil
// when the value is usable as written.
func CheckRegistryURL(raw string) *RegistryURLProblem {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil // absent means "use the built-in default"
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return &RegistryURLProblem{
			Reason: "not an absolute URL — expected a scheme and a host, e.g. https://eu-west-1.intake.senhub.io",
		}
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return &RegistryURLProblem{
			Reason: "scheme " + u.Scheme + " is not supported — expected https (or http for a local mirror)",
		}
	}

	if fixed, changed := NormalizeRegistryURL(trimmed); changed {
		return &RegistryURLProblem{
			Reason:     "carries a path the agent appends itself, so every derived URL doubles it (the agent appends /releases/... for the version list and /download/... for artifacts)",
			Suggestion: fixed,
		}
	}
	return nil
}

// warnedRegistryURLs remembers which configured values have already been
// reported, so the correction is stated once per value rather than on
// every read.
//
// It matters because the read path is hot: GetConfiguration resolves the
// auto-update block, and it is called once per datapoint batch. The
// previous version logged its warning there, so an affected host emitted
// one identical warning per batch, for the life of the process.
var warnedRegistryURLs sync.Map

// ShouldWarnRegistryURL reports whether this configured value still owes
// the operator a warning, and marks it as reported.
func ShouldWarnRegistryURL(raw string) bool {
	_, already := warnedRegistryURLs.LoadOrStore(raw, struct{}{})
	return !already
}

// ResetRegistryURLWarningsForTest clears the memo. Test-only.
func ResetRegistryURLWarningsForTest() {
	warnedRegistryURLs.Range(func(k, _ any) bool {
		warnedRegistryURLs.Delete(k)
		return true
	})
}
