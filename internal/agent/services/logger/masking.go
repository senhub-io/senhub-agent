package logger

import (
	"regexp"
	"strings"
)

// sensitivePatterns defines regex patterns to identify sensitive data
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(token|api[-_]?key|secret|authentication[-_]?key)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(Authorization|Auth):\s*(Bearer|Basic)\s+([a-zA-Z0-9+/=._-]+)`),
	regexp.MustCompile(`(?i)("[^"]*pass[^"]*":\s*")[^"]*(")`),
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
	visible := valueLen / 4
	if visible < 2 {
		visible = 2
	}
	if visible > 4 {
		visible = 4
	}
	
	prefix := value[:visible]
	suffix := value[valueLen-visible:]
	masked := strings.Repeat("*", valueLen-visible*2)
	
	return prefix + masked + suffix
}