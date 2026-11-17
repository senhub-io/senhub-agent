package secret

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"strings"
)

// illegalKeyChars matches anything outside the allowed backend-key charset
// ([A-Za-z0-9_.-]). systemd credential names also permit `:` (systemd.exec(5)),
// but the ${secret:NAME} reference grammar uses `:-` as its default-value
// separator (substitute.go), so a key containing `:-` would misparse — e.g.
// ${secret:smtp:-relay.password} reads as name "smtp" with default
// "relay.password". Excluding `:` keeps every sanitized key expressible in the
// reference grammar; a `:` in the input becomes `-` and, being lossy, gets a
// hash suffix appended below so distinct inputs never collide.
var illegalKeyChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// maxKeyLen caps the backend key length well under the systemd limit (255) for
// readability and filesystem comfort.
const maxKeyLen = 64

var keyHashEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// SanitizeKey maps an instance-qualified secret name (e.g. "veeam-prod.password"
// or "citrix-1.director.auth.password") to a backend-safe key. It replaces
// out-of-charset characters and length-caps the result. When sanitisation is
// lossy or would overflow the cap, a short deterministic hash of the ORIGINAL
// name is appended so two distinct inputs cannot collapse onto the same key.
func SanitizeKey(name string) string {
	clean := illegalKeyChars.ReplaceAllString(name, "-")
	if clean == name && len(clean) <= maxKeyLen {
		return clean
	}
	sum := sha256.Sum256([]byte(name))
	suffix := "-" + strings.ToLower(keyHashEncoding.EncodeToString(sum[:5]))
	keep := maxKeyLen - len(suffix)
	if keep < 0 {
		keep = 0
	}
	if len(clean) > keep {
		clean = clean[:keep]
	}
	return clean + suffix
}

// FleetPrefix is the part of the store reserved for the secrets a fleet
// server seals to the agent; an operator name never lands in it.
const FleetPrefix = "fleet."

// OperatorKey is SanitizeKey for a secret the operator owns (a sealed
// probe or output parameter). A key that would fall in the fleet part of
// the store, such as the secrets of a probe named "fleet", is moved under
// "local-": a fleet overlay may resolve and erase fleet.* names, never an
// operator's secret. The comparison ignores case, for case-insensitive
// file systems.
func OperatorKey(name string) string {
	key := SanitizeKey(name)
	lower := strings.ToLower(key)
	if lower == strings.TrimSuffix(FleetPrefix, ".") || strings.HasPrefix(lower, FleetPrefix) {
		return SanitizeKey("local-" + key)
	}
	return key
}
