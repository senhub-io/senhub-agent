package snmptrap

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

// encodePacket builds a real SNMP datagram of the given PDU type via
// gosnmp, so the ack tests run against wire-accurate input rather than
// hand-rolled bytes.
func encodePacket(t *testing.T, version gosnmp.SnmpVersion, pduType gosnmp.PDUType) []byte {
	t.Helper()
	g := &gosnmp.GoSNMP{
		Version:   version,
		Community: "public",
		Logger:    gosnmp.NewLogger(nil),
	}
	pdus := []gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(4242)},
		{Name: "1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: "1.3.6.1.4.1.99999.1"},
		{Name: "1.3.6.1.4.1.99999.2", Type: gosnmp.OctetString, Value: "overheat"},
	}
	raw, err := g.SnmpEncodePacket(pduType, pdus, 0, 0)
	if err != nil {
		t.Fatalf("SnmpEncodePacket(%v): %v", pduType, err)
	}
	return raw
}

func TestBuildInformAck_V2cRoundTrip(t *testing.T) {
	raw := encodePacket(t, gosnmp.Version2c, gosnmp.InformRequest)

	ack, ok := buildInformAck(raw)
	if !ok {
		t.Fatal("buildInformAck returned ok=false for a valid v2c inform")
	}
	if len(ack) != len(raw) {
		t.Fatalf("ack length %d != inform length %d (ack must be byte-identical bar the PDU tag)", len(ack), len(raw))
	}

	// Exactly one byte must differ: the PDU type tag 0xa6 -> 0xa2.
	diffs := 0
	diffIdx := -1
	for i := range raw {
		if raw[i] != ack[i] {
			diffs++
			diffIdx = i
		}
	}
	if diffs != 1 {
		t.Fatalf("ack differs from inform in %d bytes, want exactly 1", diffs)
	}
	if raw[diffIdx] != pduInformRequest || ack[diffIdx] != pduGetResponse {
		t.Fatalf("flipped byte at %d: inform=0x%02x ack=0x%02x, want 0xa6->0xa2", diffIdx, raw[diffIdx], ack[diffIdx])
	}

	// The ack must decode as a GetResponse that echoes the inform's
	// request-id and varbinds — that is what makes the sender stop
	// retransmitting.
	dec := &gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "public", Logger: gosnmp.NewLogger(nil)}
	orig, err := dec.SnmpDecodePacket(raw)
	if err != nil {
		t.Fatalf("decode inform: %v", err)
	}
	resp, err := dec.SnmpDecodePacket(ack)
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if resp.PDUType != gosnmp.GetResponse {
		t.Errorf("ack PDUType = %v, want GetResponse", resp.PDUType)
	}
	if resp.RequestID != orig.RequestID {
		t.Errorf("ack RequestID = %d, want %d (must echo the inform)", resp.RequestID, orig.RequestID)
	}
	if len(resp.Variables) != len(orig.Variables) {
		t.Errorf("ack has %d varbinds, want %d (must echo the inform)", len(resp.Variables), len(orig.Variables))
	}
}

func TestBuildInformAck_Rejects(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"too short", []byte{0x30}},
		{"not a sequence", []byte{0x02, 0x01, 0x01}},
		{"v2c trap not inform", encodePacket(t, gosnmp.Version2c, gosnmp.SNMPv2Trap)},
		{"v2c getresponse not inform", encodePacket(t, gosnmp.Version2c, gosnmp.GetResponse)},
		// Non-v2c version byte (here 3): even with an inform PDU tag, the
		// v3 scoped PDU may be encrypted, so it is not ackable.
		{"version not v2c", []byte{
			0x30, 0x08,
			0x02, 0x01, 0x03, // INTEGER version = 3
			0x04, 0x01, 0x70, // OCTET STRING community "p"
			0xa6, 0x00, // InformRequest, empty
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := buildInformAck(tc.raw); ok {
				t.Errorf("buildInformAck(%s) = ok, want rejected", tc.name)
			}
		})
	}
}

func TestASN1Len(t *testing.T) {
	cases := []struct {
		name     string
		b        []byte
		i        int
		wantLen  int
		wantNext int
		wantOK   bool
	}{
		{"short form", []byte{0x05}, 0, 5, 1, true},
		{"short form zero", []byte{0x00}, 0, 0, 1, true},
		{"long form 1 byte", []byte{0x81, 0x80}, 0, 128, 2, true},
		{"long form 2 bytes", []byte{0x82, 0x01, 0x00}, 0, 256, 3, true},
		{"truncated long form", []byte{0x82, 0x01}, 0, 0, 0, false},
		{"indefinite rejected", []byte{0x80}, 0, 0, 0, false},
		{"too many length bytes", []byte{0x85, 1, 2, 3, 4, 5}, 0, 0, 0, false},
		{"offset past end", []byte{0x05}, 1, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLen, gotNext, gotOK := asn1Len(tc.b, tc.i)
			if gotOK != tc.wantOK || gotLen != tc.wantLen || gotNext != tc.wantNext {
				t.Errorf("asn1Len(%v, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tc.b, tc.i, gotLen, gotNext, gotOK, tc.wantLen, tc.wantNext, tc.wantOK)
			}
		})
	}
}
