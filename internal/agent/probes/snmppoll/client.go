package snmppoll

import (
	"fmt"
	"math/big"
	"senhub-agent.go/internal/agent/services/snmpcore"

	"github.com/gosnmp/gosnmp"
)

// snmpVarBind is a transport-neutral view of one SNMP variable binding,
// decoupling the collector from the gosnmp types so tests can drive it
// with a fake client. OID is dotted without a leading dot. IsNumeric is
// false when the bind carried a value the metric rail cannot use (string,
// OID, IP, null, error); such binds are skipped rather than emitted as a
// misleading zero. (Non-numeric topology binds belong to the entity rail,
// not this path.)
type snmpVarBind struct {
	OID       string
	Value     float64
	IsNumeric bool
}

// snmpRawBind is an undecoded variable binding for the topology rail, which
// needs the non-numeric SYNTAXes the metric rail discards: OCTET STRING
// (chassis-id, port-id, sysName — MAC/text), INTEGER enums (subtypes),
// OBJECT IDENTIFIER. Value is gosnmp's native decode ([]byte for OctetString,
// int for Integer, string for OID); the LLDP parser renders it.
type snmpRawBind struct {
	OID   string
	Type  gosnmp.Asn1BER
	Value any
}

// snmpClient is the minimal SNMP surface the collectors depend on. The
// production implementation wraps gosnmp; tests provide a fake.
type snmpClient interface {
	// Connect opens the transport. Must be called before Get/BulkWalk.
	Connect() error
	// Get fetches the given scalar OIDs (dotted, no leading dot).
	Get(oids []string) ([]snmpVarBind, error)
	// BulkWalk walks a table column rooted at baseOID (GETBULK on v2c),
	// numeric-only (metric rail).
	BulkWalk(baseOID string) ([]snmpVarBind, error)
	// WalkRaw walks a subtree rooted at baseOID returning undecoded binds
	// (topology rail — strings, OIDs, enums included).
	WalkRaw(baseOID string) ([]snmpRawBind, error)
	// Close releases the transport.
	Close() error
}

// gosnmpClient is the production snmpClient backed by gosnmp.
type gosnmpClient struct {
	handle *gosnmp.GoSNMP
}

// newGosnmpClient builds a v2c gosnmp handle from the resolved config.
// SNMPv3 (USM) is deferred to a later lot; cfg.Version is validated to be
// v2c before this is reached.
func newGosnmpClient(cfg *config) *gosnmpClient {
	return &gosnmpClient{handle: &gosnmp.GoSNMP{
		Target:    cfg.Target,
		Port:      cfg.Port,
		Community: cfg.Community,
		Version:   gosnmp.Version2c,
		Timeout:   cfg.Timeout,
		Retries:   cfg.Retries,
		Transport: "udp",
		MaxOids:   gosnmp.MaxOids,
	}}
}

func (c *gosnmpClient) Connect() error {
	if err := c.handle.Connect(); err != nil {
		return fmt.Errorf("connecting to %s:%d: %w", c.handle.Target, c.handle.Port, err)
	}
	return nil
}

// Get chunks the OID list into requests of at most gosnmp.MaxOids so a
// large scalar set (many custom mappings) does not exceed what a device
// accepts in one PDU.
func (c *gosnmpClient) Get(oids []string) ([]snmpVarBind, error) {
	out := make([]snmpVarBind, 0, len(oids))
	for start := 0; start < len(oids); start += gosnmp.MaxOids {
		end := start + gosnmp.MaxOids
		if end > len(oids) {
			end = len(oids)
		}
		dotted := make([]string, end-start)
		for i, oid := range oids[start:end] {
			dotted[i] = "." + oid
		}
		packet, err := c.handle.Get(dotted)
		if err != nil {
			return nil, fmt.Errorf("snmp get: %w", err)
		}
		for _, pdu := range packet.Variables {
			out = append(out, pduToVarBind(pdu))
		}
	}
	return out, nil
}

func (c *gosnmpClient) BulkWalk(baseOID string) ([]snmpVarBind, error) {
	pdus, err := c.handle.BulkWalkAll("." + baseOID)
	if err != nil {
		return nil, fmt.Errorf("snmp bulkwalk %s: %w", baseOID, err)
	}
	out := make([]snmpVarBind, 0, len(pdus))
	for _, pdu := range pdus {
		out = append(out, pduToVarBind(pdu))
	}
	return out, nil
}

// WalkRaw mirrors BulkWalk but keeps every SYNTAX undecoded, for the
// topology rail. It does not filter non-numeric binds.
func (c *gosnmpClient) WalkRaw(baseOID string) ([]snmpRawBind, error) {
	pdus, err := c.handle.BulkWalkAll("." + baseOID)
	if err != nil {
		return nil, fmt.Errorf("snmp walkraw %s: %w", baseOID, err)
	}
	out := make([]snmpRawBind, 0, len(pdus))
	for _, pdu := range pdus {
		out = append(out, snmpRawBind{OID: snmpcore.TrimLeadingDot(pdu.Name), Type: pdu.Type, Value: pdu.Value})
	}
	return out, nil
}

func (c *gosnmpClient) Close() error {
	if c.handle == nil || c.handle.Conn == nil {
		return nil
	}
	return c.handle.Conn.Close()
}

// pduToVarBind decodes a gosnmp PDU into the neutral form, converting the
// integral SMI types the metric rail understands. Counter64 goes through
// big.Int to avoid the truncation that bit byte counters on busy
// interfaces. NoSuchObject/Instance, EndOfMibView, null, strings and OIDs
// come back with IsNumeric=false.
func pduToVarBind(pdu gosnmp.SnmpPDU) snmpVarBind {
	vb := snmpVarBind{OID: snmpcore.TrimLeadingDot(pdu.Name)}
	switch pdu.Type {
	case gosnmp.Counter32, gosnmp.Counter64, gosnmp.Gauge32, gosnmp.Integer, gosnmp.TimeTicks, gosnmp.Uinteger32:
		vb.Value = bigIntToFloat(gosnmp.ToBigInt(pdu.Value))
		vb.IsNumeric = true
	default:
		vb.IsNumeric = false
	}
	return vb
}

func bigIntToFloat(bi *big.Int) float64 {
	if bi == nil {
		return 0
	}
	f, _ := new(big.Float).SetInt(bi).Float64()
	return f
}
