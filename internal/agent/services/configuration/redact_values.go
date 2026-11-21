package configuration

import (
	"math"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var envRefPattern = regexp.MustCompile(`\$\{env:[^}]+\}`)

// valueFromEnv: the raw value references an environment variable; its resolved value is never shown.
func valueFromEnv(raw string) bool {
	return envRefPattern.MatchString(raw)
}

// urlPattern takes a wide span; each candidate is then parsed with net/url
// so a port, a userinfo or a trailing dot cannot hide a webhook host.
var urlPattern = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)

var (
	hostLabelPattern  = regexp.MustCompile(`^[A-Za-z0-9\-]{1,63}$`)
	alphaLabelPattern = regexp.MustCompile(`^[A-Za-z]+$`)
	uuidPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// webhookHostPath reports whether the (lowercased) host and path match a known webhook service.
func webhookHostPath(host, path string) bool {
	switch {
	case host == "hooks.slack.com":
		return true
	case (host == "discord.com" || host == "discordapp.com") && strings.HasPrefix(path, "/api/webhooks/"):
		return true
	case (strings.HasSuffix(host, ".webhook.office.com") || host == "outlook.office.com") && strings.HasPrefix(path, "/webhook"):
		return true
	case strings.HasSuffix(host, ".logic.azure.com") && strings.HasPrefix(path, "/workflows/"):
		return true
	case host == "chat.googleapis.com" && strings.HasPrefix(path, "/v1/spaces/"):
		return true
	}
	return genericHookPath(path)
}

// genericHookPath catches self-hosted webhooks (Mattermost, Rocket.Chat,
// Gitea and the like): a path segment named hooks, webhook or webhooks
// followed by a segment of 16 characters or more, the token.
func genericHookPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "hooks", "webhook", "webhooks":
			if len(parts[i+1]) >= 16 {
				return true
			}
		}
	}
	return false
}

// maskWebhookPath replaces the path of a URL on a known webhook host by "/***",
// keeping scheme and host. Works on a URL embedded in a longer string too.
func maskWebhookPath(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, maskWebhookCandidate)
}

// schemeInside finds another URL glued to the first one (a list joined by
// a comma or a semicolon), so each is judged on its own.
var schemeInside = regexp.MustCompile(`(?i)https?://`)

func maskWebhookCandidate(m string) string {
	if loc := schemeInside.FindAllStringIndex(m, -1); len(loc) > 1 {
		cut := loc[1][0]
		return maskWebhookCandidate(m[:cut]) + maskWebhookCandidate(m[cut:])
	}
	// A list separator ending the piece belongs to the text, not the URL.
	trail := ""
	if t := strings.TrimRight(m, ",;"); t != m {
		trail, m = m[len(t):], t
	}
	u, err := url.Parse(m)
	if err != nil || u.Host == "" || strings.ContainsAny(u.Hostname(), ",;@ ") {
		// Fail closed: an URL that does not parse cleanly keeps only its
		// scheme, so a malformed webhook cannot slip through unmasked.
		if k := strings.Index(m, "://"); k >= 0 {
			return m[:k+3] + "***" + trail
		}
		return "***" + trail
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !webhookHostPath(host, u.Path) {
		return m + trail
	}
	// Keep scheme and host (with its port); drop userinfo, path,
	// query and fragment, which may all carry the token.
	return u.Scheme + "://" + u.Host + "/***" + trail
}

var secretPrefixes = []string{
	"ghp_", "gho_", "ghs_", "ghu_", "github_pat_",
	"glpat-",
	"xoxb-", "xoxp-", "xoxa-", "xapp-",
	"sk-", "sk_live_", "rk_live_",
	"AKIA", "ASIA", "AIza", "SG.",
	"eyJ",
}

// isCanonicalUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID.
func isCanonicalUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

// isHostname reports whether s looks like a hostname: dot-separated labels of
// [A-Za-z0-9-] (1-63 each), at least one dot, last label fully alphabetic.
func isHostname(s string) bool {
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !hostLabelPattern.MatchString(l) {
			return false
		}
	}
	return alphaLabelPattern.MatchString(labels[len(labels)-1])
}

// shannonEntropy returns the Shannon entropy of s in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// looksLikeSecret reports a value that a configuration view must not show
// even under a harmless key.
func looksLikeSecret(s string) bool {
	// 1. Known prefixes.
	if len(s) >= 16 {
		for _, p := range secretPrefixes {
			if strings.HasPrefix(s, p) {
				if p == "eyJ" {
					// JWT: additionally require at least two dots.
					if strings.Count(s, ".") >= 2 {
						return true
					}
					continue
				}
				return true
			}
		}
	}

	// 2. High-entropy opaque value.
	if len(s) < 24 {
		return false
	}
	if strings.ContainsAny(s, " /:") {
		return false
	}
	hasLetter, hasDigit := false, false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '+' || r == '=' || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	if shannonEntropy(s) < 3.5 {
		return false
	}
	if isCanonicalUUID(s) || isHostname(s) {
		return false
	}
	return true
}
