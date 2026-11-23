package ebpfproto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Request{V: Version, Op: OpSnapshot}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "{\"v\":1,\"op\":\"snapshot\"}\n" {
		t.Fatalf("wire form changed: %q", buf.String())
	}
	req, err := ReadRequest(NewReader(&buf))
	if err != nil || req.Op != OpSnapshot {
		t.Fatalf("%+v %v", req, err)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	in := Response{V: Version, OK: true, Op: OpSnapshot, Dropped: 3, Evicted: 1, Tracked: 2, Conns: []Conn{{
		PID: 10, Comm: "curl", Family: 4, LocalAddr: "10.0.0.5", RemoteAddr: "10.0.0.9",
		LocalPort: 40000, RemotePort: 443, State: StateEstablished, Retransmits: 2, StartedMS: 5,
	}}}
	var buf bytes.Buffer
	if err := Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadResponse(NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Conns) != 1 || out.Conns[0] != in.Conns[0] || out.Dropped != 3 || out.Evicted != 1 {
		t.Fatalf("%+v", out)
	}
}

func TestVersionMismatch(t *testing.T) {
	if _, err := ReadRequest(NewReader(strings.NewReader("{\"v\":2,\"op\":\"hello\"}\n"))); !errors.Is(err, ErrVersion) {
		t.Fatalf("request: %v", err)
	}
	if _, err := ReadResponse(NewReader(strings.NewReader("{\"v\":2,\"ok\":true}\n"))); !errors.Is(err, ErrVersion) {
		t.Fatalf("response: %v", err)
	}
}

func TestRefusedResponseIsAnError(t *testing.T) {
	_, err := ReadResponse(NewReader(strings.NewReader("{\"v\":1,\"ok\":false,\"error\":\"unknown operation\"}\n")))
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("%v", err)
	}
}

func TestMalformedAndOversizedLines(t *testing.T) {
	if _, err := ReadRequest(NewReader(strings.NewReader("not json\n"))); err == nil {
		t.Fatal("garbage must be rejected")
	}
	huge := strings.Repeat("x", MaxLine+1) + "\n"
	if _, err := ReadLine(NewReader(strings.NewReader(huge))); err == nil {
		t.Fatal("an oversized line must be rejected")
	}
}
