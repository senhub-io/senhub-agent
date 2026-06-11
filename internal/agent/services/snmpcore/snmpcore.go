// Package snmpcore holds the SNMP helpers shared by the snmppoll and
// snmptrap probes (audit directive DP-10, #291): value rendering,
// printability, version mapping and the v3 USM protocol tables. Before
// this package the two probes had diverged — two isPrintable semantics,
// duplicated hex/octet/version helpers, USM tables only in snmptrap.
//
// One printability semantics, by decision: a byte slice is printable
// when, after stripping trailing NULs (common device padding), it is
// valid UTF-8 whose runes are all unicode-printable or tab/LF/CR. This
// reconciles the old rules: snmppoll's strict ASCII gains UTF-8 text
// and tab/LF/CR; snmptrap gains trailing-NUL tolerance and stops
// accepting bare control bytes (0x0b, 0x0c) that only looked printable
// because its lower-bound check had no upper bound.
package snmpcore

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"
)

// IsPrintable reports whether b renders safely as text. Trailing NULs
// are tolerated (stripped by the callers that render).
func IsPrintable(b []byte) bool {
	b = trimTrailingNULs(b)
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func trimTrailingNULs(b []byte) []byte {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	return b[:end]
}

// OctetText renders an OCTET STRING for the topology/entity rail: text
// when printable (trailing NULs stripped), bare lowercase hex otherwise.
func OctetText(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if IsPrintable(b) {
		return string(trimTrailingNULs(b))
	}
	return hex.EncodeToString(b)
}

// FormatPDUValue renders a gosnmp varbind value for the log rail.
// gosnmp decodes natively: []byte for OCTET STRING, int/uint variants
// for the numeric SYNTAXes, string for OBJECT IDENTIFIER, *big.Int for
// Counter64. Non-printable OCTET STRINGs are hex-encoded with a "0x"
// prefix so a binary value never corrupts the log line.
func FormatPDUValue(vb gosnmp.SnmpPDU) string {
	switch v := vb.Value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		if IsPrintable(v) {
			return string(trimTrailingNULs(v))
		}
		return "0x" + hex.EncodeToString(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case *big.Int:
		if v == nil {
			return ""
		}
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// AsBytes coerces a decoded varbind value to bytes ([]byte or string).
func AsBytes(v any) []byte {
	switch b := v.(type) {
	case []byte:
		return b
	case string:
		return []byte(b)
	default:
		return nil
	}
}

// AsInt coerces the numeric varbind types to int.
func AsInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}

// TrimLeadingDot normalizes an OID: surrounding whitespace and the
// optional leading dot are removed (config values carry spaces; PDU
// names never do).
func TrimLeadingDot(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), ".")
}

// ParseVersion maps a config version string to the gosnmp enum. The
// per-probe support window (snmppoll is v2c-only today) is the probe's
// policy, layered on top of this parse.
func ParseVersion(s string) (gosnmp.SnmpVersion, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "v1":
		return gosnmp.Version1, nil
	case "2", "2c", "v2c":
		return gosnmp.Version2c, nil
	case "3", "v3":
		return gosnmp.Version3, nil
	default:
		return 0, fmt.Errorf("unsupported SNMP version %q", s)
	}
}

// VersionString renders the gosnmp enum back to the canonical config
// form.
func VersionString(v gosnmp.SnmpVersion) string {
	switch v {
	case gosnmp.Version1:
		return "v1"
	case gosnmp.Version2c:
		return "v2c"
	case gosnmp.Version3:
		return "v3"
	default:
		return "unknown"
	}
}
