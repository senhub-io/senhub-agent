package oracle

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
)

// received dials a local listener through the dialer, sends the packets
// and returns what the listener read.
func received(t *testing.T, packets ...[]byte) []byte {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- nil
			return
		}
		defer c.Close()
		b, _ := io.ReadAll(c)
		got <- b
	}()

	conn, err := (&longPasswordDialer{}).DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	for _, p := range packets {
		in := append([]byte(nil), p...)
		n, err := conn.Write(in)
		if err != nil || n != len(p) {
			t.Fatalf("write = %d, %v; want %d, nil", n, err, len(p))
		}
		if !bytes.Equal(in, p) {
			t.Fatal("the caller's buffer was modified")
		}
	}
	conn.Close()
	return <-got
}

func negotiationPacket(logonTypes byte) []byte {
	caps := append([]byte(nil), ttcCapsPrefix...)
	caps[ttcLogonTypesIndex] = logonTypes
	pkt := append([]byte{0, 0, 0x0a, 0x51, 6, 0, 0, 0, 0, 0, 2, 105, 3, 105, 3, 3, 45}, caps...)
	return append(pkt, 0xAA, 0xBB)
}

func TestLongPasswordDialer_RaisesTheLogonTypesBit(t *testing.T) {
	connect := []byte("(DESCRIPTION=(CONNECT_DATA=(SERVICE_NAME=X)))")
	got := received(t, connect, negotiationPacket(106))

	want := append(append([]byte(nil), connect...), negotiationPacket(106|ttcLongPasswordBit)...)
	if !bytes.Equal(got, want) {
		t.Fatalf("wire bytes differ:\n got %v\nwant %v", got, want)
	}
}

func TestLongPasswordDialer_LeavesOtherPacketsAlone(t *testing.T) {
	other := []byte("select 1 from dual, no capabilities in here")
	if got := received(t, other, other); !bytes.Equal(got, append(append([]byte(nil), other...), other...)) {
		t.Fatalf("unrelated packets were altered: %v", got)
	}
}

func TestLongPasswordDialer_PatchesOnlyTheFirstNegotiation(t *testing.T) {
	got := received(t, negotiationPacket(106), negotiationPacket(106))
	want := append(negotiationPacket(106|ttcLongPasswordBit), negotiationPacket(106)...)
	if !bytes.Equal(got, want) {
		t.Fatal("the second negotiation packet was patched")
	}
}
