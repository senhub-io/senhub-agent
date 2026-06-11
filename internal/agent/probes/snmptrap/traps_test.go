package snmptrap

import (
	"testing"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/services/agentstate"
)

func TestPacketToLogRecord_KnownTrap(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(99)},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.6.3.1.1.5.3"}, // linkDown
			{Name: ".1.3.6.1.2.1.2.2.1.1.2", Type: gosnmp.Integer, Value: 2},                               // ifIndex
		},
	}
	rec := packetToLogRecord(pkt, "10.0.0.9", "trap_rx", nil)

	if rec.ProducerProbeType != ProbeType {
		t.Errorf("ProducerProbeType = %q", rec.ProducerProbeType)
	}
	if rec.Attributes["trap_oid"] != "1.3.6.1.6.3.1.1.5.3" {
		t.Errorf("trap_oid = %q", rec.Attributes["trap_oid"])
	}
	if rec.Attributes["trap_name"] != "linkDown" {
		t.Errorf("trap_name = %q, want linkDown", rec.Attributes["trap_name"])
	}
	if rec.Attributes["source_ip"] != "10.0.0.9" {
		t.Errorf("source_ip = %q", rec.Attributes["source_ip"])
	}
	if rec.Attributes["snmp_version"] != "v2c" {
		t.Errorf("snmp_version = %q", rec.Attributes["snmp_version"])
	}
	if rec.Attributes["varbind.1.3.6.1.2.1.2.2.1.1.2"] != "2" {
		t.Errorf("ifIndex varbind missing/wrong: %v", rec.Attributes)
	}
	if rec.Severity != agentstate.LogSeverityWarn {
		t.Errorf("linkDown should be WARN, got %v", rec.Severity)
	}
	// snmpTrapOID and sysUpTime must not appear as generic varbinds.
	if _, leaked := rec.Attributes["varbind.1.3.6.1.6.3.1.1.4.1.0"]; leaked {
		t.Error("snmpTrapOID leaked as a varbind")
	}
}

func TestPacketToLogRecord_UnknownTrapIsInfo(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.9999.1.2.3"},
		},
	}
	rec := packetToLogRecord(pkt, "10.0.0.5", "trap_rx", nil)
	if rec.Attributes["trap_name"] != "unknown" {
		t.Errorf("enterprise trap_name = %q, want unknown", rec.Attributes["trap_name"])
	}
	if rec.Attributes["trap_oid"] != "1.3.6.1.4.1.9999.1.2.3" {
		t.Errorf("trap_oid = %q", rec.Attributes["trap_oid"])
	}
	if rec.Severity != agentstate.LogSeverityInfo {
		t.Errorf("unknown trap should default to INFO, got %v", rec.Severity)
	}
}

func TestPacketToLogRecord_NilDoesNotCrash(t *testing.T) {
	rec := packetToLogRecord(nil, "1.2.3.4", "trap_rx", nil)
	if rec.Attributes["trap_oid"] != "" || rec.Attributes["trap_name"] != "unknown" {
		t.Errorf("nil packet should yield empty trap_oid/unknown name: %v", rec.Attributes)
	}
	if rec.Attributes["source_ip"] != "1.2.3.4" {
		t.Errorf("source_ip should still be set: %v", rec.Attributes)
	}
}
