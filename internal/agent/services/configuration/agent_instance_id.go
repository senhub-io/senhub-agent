package configuration

import (
	"crypto/sha1" // #nosec G505 - RFC 4122 name-based UUID v5 is defined over SHA-1; see AgentInstanceID
	"encoding/hex"
)

// agentInstanceNamespace is the RFC 4122 namespace the agent's
// service.instance.id is derived in. Changing it re-keys every agent in
// every backend; it never changes.
var agentInstanceNamespace = [16]byte{0xea, 0x2b, 0x38, 0xe0, 0xe9, 0x7e, 0x41, 0xa0, 0xb0, 0x91, 0xd2, 0x4e, 0xf1, 0xa3, 0x06, 0x71}

// AgentInstanceID is the agent's exported identity: the service.instance.id
// on every OTLP resource and the id of its service.instance entity. The
// semantic conventions ask for an RFC 4122 UUID there; this is a name-based
// UUID v5 of the agent key, stable for as long as the key is.
//
// The derivation is one-way only because the key carries enough entropy:
// the agent generates it as a random UUID (122 bits). A short or guessable
// key would make this value an offline check for guesses (hash a
// candidate, compare). Keep agent keys random UUIDs.
func AgentInstanceID(agentKey string) string {
	if agentKey == "" {
		return ""
	}
	h := sha1.New() // #nosec G401 - UUID v5 by definition; the secrecy rests on the key's entropy, not on the hash
	h.Write(agentInstanceNamespace[:])
	h.Write([]byte(agentKey))
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	s := hex.EncodeToString(u[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
