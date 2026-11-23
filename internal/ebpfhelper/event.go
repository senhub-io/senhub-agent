// Package ebpfhelper holds the platform-independent logic of the senhub-ebpf
// helper: decoding kernel events, tracking connections within hard bounds,
// answering the agent over a unix socket, and the start-up checks. Everything
// that touches the kernel lives in cmd/senhub-ebpf.
package ebpfhelper

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

// Kinds of event emitted by the kernel programs.
const (
	KindState   = 1
	KindRetrans = 2
)

// TCP states, as numbered by the kernel.
const (
	StateEstablished = 1
	StateSynSent     = 2
	StateSynRecv     = 3
	StateFinWait1    = 4
	StateFinWait2    = 5
	StateTimeWait    = 6
	StateClose       = 7
	StateCloseWait   = 8
	StateLastAck     = 9
	StateListen      = 10
	StateClosing     = 11
)

// EventSize is the size of the record the kernel programs write; it must equal
// sizeof(struct conn_event) in bpf/conn.c.
const EventSize = 80

// Event is one decoded kernel record.
type Event struct {
	SkAddr   uint64
	PID      uint32 // thread id
	TGID     uint32 // process id
	Sport    uint16
	Dport    uint16
	Family   uint16
	Kind     uint8
	OldState uint8
	NewState uint8
	Saddr    netip.Addr
	Daddr    netip.Addr
	Comm     string
}

// DecodeEvent parses one ring-buffer record in the byte order of the host.
func DecodeEvent(b []byte) (Event, error) {
	if len(b) < EventSize {
		return Event{}, fmt.Errorf("event of %d bytes, want %d", len(b), EventSize)
	}
	bo := binary.NativeEndian
	ev := Event{
		SkAddr:   bo.Uint64(b[0:]),
		PID:      bo.Uint32(b[8:]),
		TGID:     bo.Uint32(b[12:]),
		Sport:    bo.Uint16(b[16:]),
		Dport:    bo.Uint16(b[18:]),
		Family:   bo.Uint16(b[20:]),
		Kind:     b[22],
		OldState: b[23],
		NewState: b[24],
	}
	switch ev.Family {
	case 2:
		ev.Saddr = netip.AddrFrom4([4]byte(b[32:36]))
		ev.Daddr = netip.AddrFrom4([4]byte(b[48:52]))
	case 10:
		ev.Saddr = netip.AddrFrom16([16]byte(b[32:48]))
		ev.Daddr = netip.AddrFrom16([16]byte(b[48:64]))
	default:
		return Event{}, fmt.Errorf("unsupported address family %d", ev.Family)
	}
	comm := b[64:80]
	n := 0
	for n < len(comm) && comm[n] != 0 {
		n++
	}
	ev.Comm = string(comm[:n])
	return ev, nil
}

var stateNames = map[uint8]string{
	StateEstablished: "ESTABLISHED",
	StateSynSent:     "SYN_SENT",
	StateSynRecv:     "SYN_RECV",
	StateFinWait1:    "FIN_WAIT1",
	StateFinWait2:    "FIN_WAIT2",
	StateTimeWait:    "TIME_WAIT",
	StateClose:       "CLOSE",
	StateCloseWait:   "CLOSE_WAIT",
	StateLastAck:     "LAST_ACK",
	StateListen:      "LISTEN",
	StateClosing:     "CLOSING",
}

// StateName returns the name of a kernel TCP state, or "UNKNOWN".
func StateName(s uint8) string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return "UNKNOWN"
}
