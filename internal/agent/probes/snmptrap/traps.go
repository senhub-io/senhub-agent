package snmptrap

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/snmpmib"
)

// Well-known OIDs from SNMPv2-MIB (RFC 3418). snmpTrapOID.0 carries the
// identifying OID of a v2c/v3 trap; sysUpTime.0 is the first varbind.
const (
	oidSnmpTrapOID = "1.3.6.1.6.3.1.1.4.1.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
)

// standardTraps maps the six generic trap OIDs (SNMPv2-MIB, RFC 3418
// §2) to human-readable names. This is a fixed, compiled-in table — the
// probe deliberately does NOT load or runtime-fetch MIB files to resolve
// names (a documented anti-pattern for this agent); enterprise-specific
// trap OIDs are surfaced by their numeric OID under trap_oid and the
// operator maps them downstream.
var standardTraps = map[string]string{
	"1.3.6.1.6.3.1.1.5.1": "coldStart",
	"1.3.6.1.6.3.1.1.5.2": "warmStart",
	"1.3.6.1.6.3.1.1.5.3": "linkDown",
	"1.3.6.1.6.3.1.1.5.4": "linkUp",
	"1.3.6.1.6.3.1.1.5.5": "authenticationFailure",
	"1.3.6.1.6.3.1.1.5.6": "egpNeighborLoss",
}

// trapSeverity assigns an OTel severity to the known generic traps.
// Traps carry no severity field, so this is a small fixed heuristic;
// unknown (enterprise) traps default to INFO — the operator escalates
// downstream by trap_oid.
func trapSeverity(trapOID string) (agentstate.LogSeverity, string) {
	switch standardTraps[trapOID] {
	case "linkDown", "authenticationFailure", "egpNeighborLoss":
		return agentstate.LogSeverityWarn, "WARN"
	default:
		return agentstate.LogSeverityInfo, "INFO"
	}
}

// normalizeOID strips a single leading dot. gosnmp renders OIDs as
// ".1.3.6..."; the agent keys on the dotless form.
func normalizeOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

// packetToLogRecord converts a received SNMP trap/inform packet into an
// OTel-shaped LogRecord. It is the OS- and transport-agnostic core,
// unit-tested against synthetic packets without a live listener.
//
// Attribute keys follow the issue #161 mandated set (trap_oid, trap_name,
// source_ip) plus snmp_version and one varbind.<oid> per binding. A
// malformed packet (nil, no snmpTrapOID varbind) still yields a record so
// nothing is silently dropped — trap_oid is left empty and trap_name is
// "unknown".
func packetToLogRecord(s *gosnmp.SnmpPacket, sourceIP, probeName string, mibs *snmpmib.Resolver) agentstate.LogRecord {
	attrs := map[string]string{"source_ip": sourceIP}

	trapOID := ""
	varbindCount := 0
	if s != nil {
		attrs["snmp_version"] = snmpVersionString(s.Version)
		for _, vb := range s.Variables {
			oid := normalizeOID(vb.Name)
			if oid == oidSnmpTrapOID {
				if v, ok := vb.Value.(string); ok {
					trapOID = normalizeOID(v)
				}
				continue
			}
			if oid == oidSysUpTime {
				attrs["sysuptime"] = formatVarbindValue(vb)
				continue
			}
			// Key the varbind by its operator-MIB name when one resolves
			// (e.g. varbind.ifOperStatus.3), else by its numeric OID.
			key := "varbind." + oid
			if label, ok := mibs.Resolve(oid); ok {
				key = "varbind." + label
			}
			attrs[key] = formatVarbindValue(vb)
			varbindCount++
		}
	}

	// trap_name precedence: operator-MIB name > built-in generic trap >
	// "unknown". trap_oid always carries the numeric identity.
	trapName := ""
	if label, ok := mibs.Resolve(trapOID); ok && trapOID != "" {
		trapName = label
	} else if name := standardTraps[trapOID]; name != "" {
		trapName = name
	} else {
		trapName = "unknown"
	}
	attrs["trap_oid"] = trapOID
	attrs["trap_name"] = trapName

	sev, sevText := trapSeverity(trapOID)

	var body strings.Builder
	body.WriteString("SNMP trap ")
	if trapOID != "" {
		body.WriteString(trapName)
		body.WriteString(" (")
		body.WriteString(trapOID)
		body.WriteString(")")
	} else {
		body.WriteString("(no snmpTrapOID)")
	}
	body.WriteString(" from ")
	body.WriteString(sourceIP)
	if varbindCount > 0 {
		body.WriteString(fmt.Sprintf(" with %d varbind(s)", varbindCount))
	}

	return agentstate.LogRecord{
		Timestamp:         time.Now(),
		Severity:          sev,
		SeverityText:      sevText,
		Body:              body.String(),
		Attributes:        attrs,
		ProducerProbeName: probeName,
		ProducerProbeType: ProbeType,
	}
}

// ASN.1 BER identifier octets used to walk an SNMP message far enough
// to locate the PDU type tag.
const (
	asnSequence      = 0x30 // outer message + varbind-list SEQUENCE
	asnInteger       = 0x02 // version
	asnOctetString   = 0x04 // community
	pduInformRequest = 0xa6
	pduGetResponse   = 0xa2
)

// asn1Len reads a BER length at b[i] (i points at the first length
// octet). It returns the length value and the index just past the
// length octets. Only definite short/long form up to 4 bytes is
// accepted — SNMP messages never use indefinite form. ok is false on
// truncated or unsupported input.
func asn1Len(b []byte, i int) (length, next int, ok bool) {
	if i >= len(b) {
		return 0, 0, false
	}
	n := int(b[i])
	if n < 0x80 {
		return n, i + 1, true
	}
	numBytes := n & 0x7f
	if numBytes == 0 || numBytes > 4 {
		return 0, 0, false
	}
	i++
	if i+numBytes > len(b) {
		return 0, 0, false
	}
	val := 0
	for k := 0; k < numBytes; k++ {
		val = val<<8 | int(b[i+k])
	}
	return val, i + numBytes, true
}

// buildInformAck turns a received v2c InformRequest datagram into its
// GetResponse acknowledgement. An inform and its ack are identical on
// the wire except for the PDU type tag (0xa6 -> 0xa2): the request-id,
// community, version, varbinds and every length octet are preserved,
// and error-status / error-index are already zero in a request. So we
// walk the outer SEQUENCE { version, community, PDU } to find the PDU
// tag and flip just that one byte in a copy — no fragile re-encoding of
// arbitrary varbind values.
//
// Returns ok=false (caller sends no ack) for anything that is not a
// well-formed v2c inform. v3 informs are not acked: the scoped PDU may
// be encrypted, and v3 trap handling is best-effort.
func buildInformAck(raw []byte) ([]byte, bool) {
	// Outer message SEQUENCE.
	if len(raw) < 2 || raw[0] != asnSequence {
		return nil, false
	}
	_, pos, ok := asn1Len(raw, 1)
	if !ok {
		return nil, false
	}

	// version INTEGER — value 1 identifies v2c.
	if pos >= len(raw) || raw[pos] != asnInteger {
		return nil, false
	}
	vLen, vPos, ok := asn1Len(raw, pos+1)
	if !ok || vLen != 1 || vPos+vLen > len(raw) {
		return nil, false
	}
	if raw[vPos] != 0x01 {
		return nil, false // not v2c
	}
	pos = vPos + vLen

	// community OCTET STRING — skip its value.
	if pos >= len(raw) || raw[pos] != asnOctetString {
		return nil, false
	}
	cLen, cPos, ok := asn1Len(raw, pos+1)
	if !ok || cPos+cLen > len(raw) {
		return nil, false
	}
	pos = cPos + cLen

	// PDU tag — must be an InformRequest to be ackable here.
	if pos >= len(raw) || raw[pos] != pduInformRequest {
		return nil, false
	}

	ack := make([]byte, len(raw))
	copy(ack, raw)
	ack[pos] = pduGetResponse
	return ack, true
}

func snmpVersionString(v gosnmp.SnmpVersion) string {
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

// formatVarbindValue renders a gosnmp varbind value to a string. gosnmp
// decodes natively: []byte for OCTET STRING, int/uint variants for the
// numeric SYNTAXes, string for OBJECT IDENTIFIER, *big.Int for Counter64.
// OCTET STRINGs that are not valid printable UTF-8 are hex-encoded so a
// binary value never corrupts the log line.
func formatVarbindValue(vb gosnmp.SnmpPDU) string {
	switch v := vb.Value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		if utf8.Valid(v) && isPrintable(v) {
			return string(v)
		}
		return "0x" + hexEncode(v)
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

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0x0f]
	}
	return string(out)
}
