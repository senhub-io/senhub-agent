package snmptrap

import (
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// Security regression tests for #263: the v2c community was documented
// as authenticating receivers but never compared, and a decode panic on
// attacker-controlled bytes killed the whole agent.

func newTestTrapProbe(t *testing.T) *SNMPTrapProbe {
	t.Helper()
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	probe, err := NewSNMPTrapProbe(map[string]interface{}{
		"bind_address": "127.0.0.1:0",
		"version":      "v2c",
		"community":    "s3cret",
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewSNMPTrapProbe: %v", err)
	}
	p, ok := probe.(*SNMPTrapProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	params, err := p.buildParams()
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}
	p.params = params
	return p
}

func testRemote() *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP("10.9.8.7"), Port: 39999}
}

// drainLogs subscribes to the agentstate rail and returns a closure
// counting records received since the call.
func subscribeCount(t *testing.T) func() int {
	t.Helper()
	ch := agentstate.SubscribeLogs(64)
	t.Cleanup(func() { agentstate.UnsubscribeLogs(ch) })
	return func() int {
		n := 0
		for {
			select {
			case <-ch:
				n++
			case <-time.After(50 * time.Millisecond):
				return n
			}
		}
	}
}

func TestProcessDatagram_RejectsMismatchedCommunity(t *testing.T) {
	p := newTestTrapProbe(t)
	count := subscribeCount(t)

	p.decode = func([]byte, bool) (*gosnmp.SnmpPacket, error) {
		return &gosnmp.SnmpPacket{
			Version:   gosnmp.Version2c,
			Community: "forged",
			PDUType:   gosnmp.SNMPv2Trap,
		}, nil
	}
	p.processDatagram([]byte{0x30}, testRemote())

	if got := p.rejectedCommunity.Load(); got != 1 {
		t.Errorf("rejectedCommunity = %d, want 1", got)
	}
	if n := count(); n != 0 {
		t.Errorf("forged-community trap was published (%d records) — community check bypassed", n)
	}
}

func TestProcessDatagram_AcceptsMatchingCommunity(t *testing.T) {
	p := newTestTrapProbe(t)
	count := subscribeCount(t)

	p.decode = func([]byte, bool) (*gosnmp.SnmpPacket, error) {
		return &gosnmp.SnmpPacket{
			Version:   gosnmp.Version2c,
			Community: "s3cret",
			PDUType:   gosnmp.SNMPv2Trap,
			Variables: []gosnmp.SnmpPDU{
				{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.6.3.1.1.5.3"},
			},
		}, nil
	}
	p.processDatagram([]byte{0x30}, testRemote())

	if got := p.rejectedCommunity.Load(); got != 0 {
		t.Errorf("rejectedCommunity = %d, want 0", got)
	}
	if n := count(); n != 1 {
		t.Errorf("expected 1 published record, got %d", n)
	}
}

func TestProcessDatagram_RecoversDecodePanic(t *testing.T) {
	p := newTestTrapProbe(t)

	p.decode = func([]byte, bool) (*gosnmp.SnmpPacket, error) {
		panic("hostile BER")
	}
	// Must not propagate — one datagram panic used to kill the agent.
	p.processDatagram([]byte{0xff, 0xfe}, testRemote())

	if got := p.decodePanics.Load(); got != 1 {
		t.Errorf("decodePanics = %d, want 1", got)
	}
}

// TestProcessDatagram_HostileBytes feeds adversarial raw datagrams
// through the REAL gosnmp decoder: none may panic or publish.
func TestProcessDatagram_HostileBytes(t *testing.T) {
	p := newTestTrapProbe(t)
	count := subscribeCount(t)

	hostile := [][]byte{
		{},                           // empty
		{0x00},                       // not BER
		{0x30, 0x82, 0xff, 0xff},     // truncated long-form length
		{0x30, 0x03, 0x02, 0x01},     // truncated integer
		[]byte("GET / HTTP/1.1\r\n"), // wrong protocol entirely
		func() []byte { // deep nesting
			b := make([]byte, 0, 512)
			for i := 0; i < 128; i++ {
				b = append(b, 0x30, 0x82)
			}
			return b
		}(),
	}
	for i, msg := range hostile {
		p.processDatagram(msg, testRemote())
		_ = i
	}
	if n := count(); n != 0 {
		t.Errorf("hostile bytes produced %d published records", n)
	}
}

func TestCollect_EmitsReceiverSelfMetrics(t *testing.T) {
	p := newTestTrapProbe(t)
	p.rejectedCommunity.Store(7)
	p.decodePanics.Store(2)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float32{}
	for _, dp := range points {
		got[dp.Name] = dp.Value
	}
	if got["senhub.snmp_trap.rejected_community"] != 7 {
		t.Errorf("rejected_community = %v, want 7", got["senhub.snmp_trap.rejected_community"])
	}
	if got["senhub.snmp_trap.decode_panics"] != 2 {
		t.Errorf("decode_panics = %v, want 2", got["senhub.snmp_trap.decode_panics"])
	}
}
