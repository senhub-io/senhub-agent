package common

import (
	"strings"
)

// CanonicalHostID renders a machine identifier in the form the entity
// contract specifies for host.id: lowercase UUID with dashes (8-4-4-4-12).
// It accepts 32 hexadecimal characters or the dashed form, in any case, and
// optionally wrapped in braces. Any other value is returned unchanged with
// ok=false so the caller can log it once and keep the identity it already
// emits.
//
// Not wired into the emission sites: gopsutil already returns the canonical
// form on Linux, Windows and macOS, and applying this to an agent that emits
// another form would re-key its host in the consumer.
func CanonicalHostID(raw string) (id string, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	flat := strings.ReplaceAll(s, "-", "")
	if len(flat) != 32 || !isHex(flat) {
		return raw, false
	}
	if strings.Contains(s, "-") && len(s) != 36 {
		return raw, false
	}
	return flat[0:8] + "-" + flat[8:12] + "-" + flat[12:16] + "-" + flat[16:20] + "-" + flat[20:32], true
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
