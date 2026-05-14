package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Compact license format: ~42 chars human-readable key
// Format: SH-XXXXXX-XXXXXX-XXXXXX-XXXXXX-XXXXXX
//
// Binary layout (20 bytes):
//   [0]     version (0x01)
//   [1]     tier (0=Free, 1=Pro, 2=Enterprise)
//   [2:4]   expiry days since 2024-01-01 (uint16, big-endian)
//   [4:8]   probe bitmap (uint32, big-endian)
//   [8:12]  agent hash (first 4 bytes of SHA256 of subject)
//   [12:20] HMAC-SHA256 truncated to 8 bytes

const (
	compactVersion    = 0x01
	compactPrefix     = "SH"
	compactPayloadLen = 12
	compactSigLen     = 8
	compactTotalLen   = compactPayloadLen + compactSigLen // 20 bytes
)

// Epoch for compact license dates
var compactEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// HMAC key embedded in the agent binary for compact license validation.
// This is a shared secret — sufficient for license validation but not
// cryptographically equivalent to RSA asymmetric signing.
var compactHMACKey = []byte("senhub-compact-license-v1-hmac-key-2024")

// Probe type to bit position mapping (max 32 probes)
var probeBitmap = map[string]uint{
	"cpu":                  0,
	"memory":               1,
	"logicaldisk":          2,
	"network":              3,
	"ping_gateway":         4,
	"ping_webapp":          5,
	"load_webapp":          6,
	"wifi_signal_strength": 7,
	"syslog":               8,
	"event":                9,
	// bit 10 reserved (formerly otel; probe removed pending implementation)
	"redfish":   11,
	"citrix":    12,
	"netscaler": 13,
	"veeam":     14,
	// 15-31 reserved for future probes
}

// Reverse bitmap for decoding
var bitToProbe map[uint]string

func init() {
	bitToProbe = make(map[uint]string, len(probeBitmap))
	for name, bit := range probeBitmap {
		bitToProbe[bit] = name
	}
}

// IsCompactLicense returns true if the token looks like a compact license key
func IsCompactLicense(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), compactPrefix+"-")
}

// GenerateCompactLicense creates a compact license key from license parameters.
// Used by the license generator tool.
func GenerateCompactLicense(tier LicenseTier, expiresAt time.Time, authorizedProbes []string, subject string) (string, error) {
	payload := encodePayload(tier, expiresAt, authorizedProbes, subject)

	sig := computeHMAC(payload)

	raw := make([]byte, compactTotalLen)
	copy(raw[:compactPayloadLen], payload)
	copy(raw[compactPayloadLen:], sig)

	return formatCompactKey(raw), nil
}

// ValidateCompactLicense validates a compact license key and returns a License
func ValidateCompactLicense(token string) (*License, error) {
	raw, err := parseCompactKey(token)
	if err != nil {
		return nil, fmt.Errorf("invalid compact license format: %w", err)
	}

	if len(raw) != compactTotalLen {
		return nil, fmt.Errorf("invalid compact license length: expected %d, got %d", compactTotalLen, len(raw))
	}

	// Split payload and signature
	payload := raw[:compactPayloadLen]
	sig := raw[compactPayloadLen:]

	// Verify HMAC
	expectedSig := computeHMAC(payload)
	if !hmac.Equal(sig, expectedSig) {
		return nil, fmt.Errorf("invalid compact license signature")
	}

	// Decode payload
	return decodePayload(payload)
}

func encodePayload(tier LicenseTier, expiresAt time.Time, authorizedProbes []string, subject string) []byte {
	payload := make([]byte, compactPayloadLen)

	// Version
	payload[0] = compactVersion

	// Tier
	switch tier {
	case TierFree:
		payload[1] = 0
	case TierPro:
		payload[1] = 1
	case TierEnterprise:
		payload[1] = 2
	}

	// Expiry: days since epoch
	days := uint16(expiresAt.Sub(compactEpoch).Hours() / 24)
	binary.BigEndian.PutUint16(payload[2:4], days)

	// Probe bitmap
	var bitmap uint32
	for _, probe := range authorizedProbes {
		if probe == "*" {
			bitmap = 0xFFFFFFFF // All probes
			break
		}
		if bit, ok := probeBitmap[probe]; ok {
			bitmap |= 1 << bit
		}
	}
	binary.BigEndian.PutUint32(payload[4:8], bitmap)

	// Agent hash (first 4 bytes of SHA256 of subject)
	h := sha256.Sum256([]byte(subject))
	copy(payload[8:12], h[:4])

	return payload
}

func decodePayload(payload []byte) (*License, error) {
	if payload[0] != compactVersion {
		return nil, fmt.Errorf("unsupported compact license version: %d", payload[0])
	}

	// Tier
	var tier LicenseTier
	switch payload[1] {
	case 0:
		tier = TierFree
	case 1:
		tier = TierPro
	case 2:
		tier = TierEnterprise
	default:
		return nil, fmt.Errorf("unknown tier: %d", payload[1])
	}

	// Expiry
	days := binary.BigEndian.Uint16(payload[2:4])
	expiresAt := compactEpoch.Add(time.Duration(days) * 24 * time.Hour)
	isExpired := time.Now().After(expiresAt)

	// Probe bitmap
	bitmap := binary.BigEndian.Uint32(payload[4:8])
	var probes []string
	if bitmap == 0xFFFFFFFF {
		probes = []string{"*"}
	} else {
		for bit := uint(0); bit < 32; bit++ {
			if bitmap&(1<<bit) != 0 {
				if name, ok := bitToProbe[bit]; ok {
					probes = append(probes, name)
				}
			}
		}
	}

	return &License{
		Tier:             tier,
		AuthorizedProbes: probes,
		ExpiresAt:        expiresAt,
		IssuedAt:         time.Now(), // Not stored in compact format
		IsExpired:        isExpired,
		GracePeriodDays:  7,
	}, nil
}

// VerifyBinding checks that a license token is bound to the given agent key.
// For compact: compares the embedded agent hash.
// For JWT: compares the Subject claim.
func VerifyBinding(token string, agentKey string, lic *License) bool {
	if IsCompactLicense(token) {
		raw, err := parseCompactKey(token)
		if err != nil || len(raw) < compactPayloadLen {
			return false
		}
		expected := sha256.Sum256([]byte(agentKey))
		return raw[8] == expected[0] && raw[9] == expected[1] && raw[10] == expected[2] && raw[11] == expected[3]
	}
	// JWT: Subject must match agent key
	return lic.Subject == "" || lic.Subject == agentKey
}

func computeHMAC(payload []byte) []byte {
	mac := hmac.New(sha256.New, compactHMACKey)
	mac.Write(payload)
	full := mac.Sum(nil)
	return full[:compactSigLen]
}

// Base32 encoding using Crockford's alphabet (no I, L, O, U — unambiguous)
const base32Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func formatCompactKey(raw []byte) string {
	encoded := base32Encode(raw)

	// Group in chunks of 6 with dashes
	var parts []string
	for i := 0; i < len(encoded); i += 6 {
		end := i + 6
		if end > len(encoded) {
			end = len(encoded)
		}
		parts = append(parts, encoded[i:end])
	}

	return compactPrefix + "-" + strings.Join(parts, "-")
}

func parseCompactKey(key string) ([]byte, error) {
	key = strings.TrimSpace(key)

	if !strings.HasPrefix(key, compactPrefix+"-") {
		return nil, fmt.Errorf("missing SH- prefix")
	}

	// Remove prefix and dashes
	body := strings.TrimPrefix(key, compactPrefix+"-")
	body = strings.ReplaceAll(body, "-", "")
	body = strings.ToUpper(body)

	return base32Decode(body)
}

func base32Encode(data []byte) string {
	var result []byte
	buffer := uint64(0)
	bits := 0

	for _, b := range data {
		buffer = (buffer << 8) | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			idx := (buffer >> uint(bits)) & 0x1F
			result = append(result, base32Alphabet[idx])
		}
	}
	if bits > 0 {
		idx := (buffer << uint(5-bits)) & 0x1F
		result = append(result, base32Alphabet[idx])
	}

	return string(result)
}

func base32Decode(encoded string) ([]byte, error) {
	reverseAlphabet := make(map[byte]uint64)
	for i, c := range base32Alphabet {
		reverseAlphabet[byte(c)] = uint64(i)
	}

	buffer := uint64(0)
	bits := 0
	var result []byte

	for i := 0; i < len(encoded); i++ {
		val, ok := reverseAlphabet[encoded[i]]
		if !ok {
			return nil, fmt.Errorf("invalid character '%c' at position %d", encoded[i], i)
		}
		buffer = (buffer << 5) | val
		bits += 5
		if bits >= 8 {
			bits -= 8
			result = append(result, byte(buffer>>uint(bits)))
		}
	}

	return result, nil
}
