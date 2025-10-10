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

// maskValue masks text while preserving the first and last characters
func maskValue(value string) string {
	valueLen := len(value)

	// For short values, mask completely
	if valueLen <= 8 {
		return "********"
	}

	// For longer values, preserve beginning and end
	visible := 2 // Default: show 2 characters at start and end
	if valueLen > 20 {
		visible = 4 // For very long values, show 4 characters
	}

	if visible*2 >= valueLen {
		return "********"
	}

	prefix := value[:visible]
	suffix := value[valueLen-visible:]
	maskedLength := valueLen - visible*2
	masked := strings.Repeat("*", maskedLength)

	return prefix + masked + suffix
}
