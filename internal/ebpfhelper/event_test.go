package ebpfhelper

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func rawEvent(family uint16, saddr, daddr []byte, comm string) []byte {
	b := make([]byte, EventSize)
	bo := binary.NativeEndian
	bo.PutUint64(b[0:], 0xffff888012345678)
	bo.PutUint32(b[8:], 4243)
	bo.PutUint32(b[12:], 4242)
	bo.PutUint16(b[16:], 41234)
	bo.PutUint16(b[18:], 5432)
	bo.PutUint16(b[20:], family)
	b[22] = KindState
	b[23] = StateClose
	b[24] = StateSynSent
	copy(b[32:], saddr)
	copy(b[48:], daddr)
	copy(b[64:], comm)
	return b
}

func TestDecodeEventIPv4(t *testing.T) {
	ev, err := DecodeEvent(rawEvent(2, []byte{10, 0, 0, 5}, []byte{10, 0, 0, 9}, "psql"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.TGID != 4242 || ev.PID != 4243 || ev.Sport != 41234 || ev.Dport != 5432 || ev.Comm != "psql" {
		t.Fatalf("unexpected event %+v", ev)
	}
	if ev.Saddr != netip.MustParseAddr("10.0.0.5") || ev.Daddr != netip.MustParseAddr("10.0.0.9") {
		t.Fatalf("unexpected addresses %v %v", ev.Saddr, ev.Daddr)
	}
	if ev.NewState != StateSynSent || ev.OldState != StateClose || ev.Kind != KindState {
		t.Fatalf("unexpected states %+v", ev)
	}
}

func TestDecodeEventIPv6(t *testing.T) {
	s := netip.MustParseAddr("fd00::1").As16()
	d := netip.MustParseAddr("fd00::2").As16()
	ev, err := DecodeEvent(rawEvent(10, s[:], d[:], "a-very-long-comm"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Saddr.String() != "fd00::1" || ev.Daddr.String() != "fd00::2" {
		t.Fatalf("unexpected addresses %v %v", ev.Saddr, ev.Daddr)
	}
	if ev.Comm != "a-very-long-comm" {
		t.Fatalf("comm %q", ev.Comm)
	}
}

func TestDecodeEventRejectsBadInput(t *testing.T) {
	if _, err := DecodeEvent(make([]byte, EventSize-1)); err == nil {
		t.Fatal("a short record must be rejected")
	}
	if _, err := DecodeEvent(rawEvent(7, nil, nil, "")); err == nil {
		t.Fatal("an unknown family must be rejected")
	}
}

func TestStateName(t *testing.T) {
	if StateName(StateEstablished) != "ESTABLISHED" || StateName(99) != "UNKNOWN" {
		t.Fatal("state names")
	}
}
