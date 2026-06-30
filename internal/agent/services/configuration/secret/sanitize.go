package secret

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"strings"
)

// illegalKeyChars matches anything outside the systemd-credential name charset
// ([A-Za-z0-9:_.-], per systemd.exec(5)). Other backends (age key-file map,
// DPAPI map) are more permissive, so this is the lowest common denominator.
var illegalKeyChars = regexp.MustCompile(`[^A-Za-z0-9:_.-]`)

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
