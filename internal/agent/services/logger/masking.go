package logger

import (
	"regexp"
	"strings"
)

// sensitivePatterns defines regex patterns to identify sensitive data
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)"(password|passwd|pwd)"\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`(?i)"(token|api[-_]?key|secret|authentication[-_]?key)"\s*:\s*"([^"]+)"`),
	regexp.MustCompile(`(?i)(password|passwd|pwd)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(token|api[-_]?key|secret|authentication[-_]?key)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)([a-z_]*passphrase|community)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(Authorization|Auth):\s*(Bearer|Basic)\s+([a-zA-Z0-9+/=._-]+)`),
}

// MaskSensitiveData replaces sensitive information with asterisks
func MaskSensitiveData(input string) string {
	masked := input
	for _, pattern := range sensitivePatterns {
		masked = pattern.ReplaceAllStringFunc(masked, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) >= 3 {
				value := parts[len(parts)-1]
				maskedValue := maskValue(value)
				return strings.Replace(match, value, maskedValue, 1)
			}
			return match
		})
	}
	return masked
}

// maskValue masks a sensitive value in full: never a partially visible value.
func maskValue(value string) string {
	return "***"
}
