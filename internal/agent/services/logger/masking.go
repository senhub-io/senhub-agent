package logger

import (
	"regexp"
	"strings"
)

// sensitivePatterns définit les regex pour identifier les données sensibles
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(token|api[-_]?key|secret|authentication[-_]?key)["']?\s*[:=]\s*["']?([^"',\s]+)`),
	regexp.MustCompile(`(?i)(Authorization|Auth):\s*(Bearer|Basic)\s+([a-zA-Z0-9+/=._-]+)`),
	regexp.MustCompile(`(?i)("[^"]*pass[^"]*":\s*")[^"]*(")`),
}

// MaskSensitiveData remplace les informations sensibles par des astérisques
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

// maskValue masque un texte en préservant les premiers et derniers caractères
func maskValue(value string) string {
	valueLen := len(value)
	
	// Pour les valeurs courtes, masquer complètement
	if valueLen <= 8 {
		return "********"
	}
	
	// Pour les valeurs plus longues, préserver le début et la fin
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