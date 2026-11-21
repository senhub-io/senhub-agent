package configuration

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

var envRefPattern = regexp.MustCompile(`\$\{env:[^}]+\}`)

// valueFromEnv: the raw value references an environment variable; its resolved value is never shown.
func valueFromEnv(raw string) bool {
	return envRefPattern.MatchString(raw)
}

var urlPattern = regexp.MustCompile(`(?i)\bhttps?://[A-Za-z0-9.\-]+(?:/[^\s"'<>)]*)?`)

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
	}
	return false
}

// maskWebhookPath replaces the path of a URL on a known webhook host by "/***",
// keeping scheme and host. Works on a URL embedded in a longer string too.
func maskWebhookPath(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.Index(m, "://")
		if i < 0 {
			return m
		}
		scheme, rest := m[:i], m[i+3:]
		host, path := rest, ""
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			host, path = rest[:j], rest[j:]
		}
		if webhookHostPath(strings.ToLower(host), path) {
			return scheme + "://" + host + "/***"
		}
		return m
	})
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
