// Package ebpfproto is the wire protocol between the senhub-ebpf helper and
// the agent: one JSON object per line over a unix socket, one request answered
// by exactly one response.
//
// Every message carries the protocol version in "v". A peer that receives a
// version it does not know answers with ok=false and an error, and the agent
// treats the helper as absent: the version is the only compatibility switch.
package ebpfproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Version is the protocol version spoken by this build.
const Version = 1

// DefaultSocket is where the helper listens and the agent looks.
const DefaultSocket = "/run/senhub-ebpf/ebpf.sock"

// MaxLine bounds one protocol line in either direction.
const MaxLine = 16 << 20

// Operations.
const (
	OpHello    = "hello"
	OpSnapshot = "snapshot"
)

// TCP state names carried in Conn.State.
const (
	StateEstablished = "ESTABLISHED"
	StateSynSent     = "SYN_SENT"
	StateSynRecv     = "SYN_RECV"
	StateFinWait1    = "FIN_WAIT1"
	StateFinWait2    = "FIN_WAIT2"
	StateTimeWait    = "TIME_WAIT"
	StateClose       = "CLOSE"
	StateCloseWait   = "CLOSE_WAIT"
	StateLastAck     = "LAST_ACK"
	StateListen      = "LISTEN"
	StateClosing     = "CLOSING"
)

// Request is the agent's message.
type Request struct {
	V  int    `json:"v"`
	Op string `json:"op"`
}

// Conn is one TCP connection as the kernel reported it to the helper.
//
// PID is the thread-group id of the process that ran connect() and is zero when
// the helper did not see the connection start (it was open before the helper
// started, or it was accepted: the process running at accept time is not the
// owner). Comm is that process's kernel command name at connect time.
// Retransmits counts retransmitted segments seen since the connection was first
// seen. Addresses and ports are in the same orientation as the local socket:
// local first, remote second.
type Conn struct {
	PID         uint32 `json:"pid,omitempty"`
	Comm        string `json:"comm,omitempty"`
	Family      int    `json:"family"` // 4 or 6
	LocalAddr   string `json:"saddr"`
	RemoteAddr  string `json:"daddr"`
	LocalPort   uint16 `json:"sport"`
	RemotePort  uint16 `json:"dport"`
	State       string `json:"state"`
	Retransmits uint64 `json:"retransmits,omitempty"`
	Closed      bool   `json:"closed,omitempty"`
	// StartedMS is the wall-clock time, in epoch milliseconds, at which the
	// helper saw the connection begin; zero when it did not.
	StartedMS int64 `json:"started_ms,omitempty"`
}

// Response is the helper's message. Fields not relevant to the operation are
// omitted.
type Response struct {
	V     int    `json:"v"`
	OK    bool   `json:"ok"`
	Op    string `json:"op,omitempty"`
	Error string `json:"error,omitempty"`

	// hello
	Helper        string   `json:"helper,omitempty"`
	HelperVersion string   `json:"helper_version,omitempty"`
	Kernel        string   `json:"kernel,omitempty"`
	BTF           bool     `json:"btf,omitempty"`
	Programs      []string `json:"programs,omitempty"`
	UptimeSeconds int64    `json:"uptime_s,omitempty"`

	// snapshot
	Conns []Conn `json:"conns,omitempty"`
	// Dropped is the number of events the kernel could not queue (ring buffer
	// full) since the helper started; Evicted is the number of tracked
	// connections the helper's size cap pushed out. Both only grow.
	Dropped uint64 `json:"dropped,omitempty"`
	Evicted uint64 `json:"evicted,omitempty"`
	Tracked int    `json:"tracked,omitempty"`
}

// ErrVersion is returned when a peer speaks another protocol version.
var ErrVersion = errors.New("unsupported protocol version")

// Write encodes v as one line.
func Write(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding message: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	return nil
}

// NewReader returns a line reader sized for protocol messages.
func NewReader(r io.Reader) *bufio.Reader { return bufio.NewReaderSize(r, 64<<10) }

// ReadLine reads one line of at most MaxLine bytes, without its terminator.
func ReadLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > MaxLine {
			return nil, fmt.Errorf("line longer than %d bytes", MaxLine)
		}
		if !isPrefix {
			return line, nil
		}
	}
}

// ReadRequest reads and validates one request.
func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := ReadLine(r)
	if err != nil {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, fmt.Errorf("decoding request: %w", err)
	}
	if req.V != Version {
		return req, fmt.Errorf("%w: %d", ErrVersion, req.V)
	}
	return req, nil
}

// ReadResponse reads and validates one response.
func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := ReadLine(r)
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("decoding response: %w", err)
	}
	if resp.V != Version {
		return resp, fmt.Errorf("%w: %d", ErrVersion, resp.V)
	}
	if !resp.OK {
		return resp, fmt.Errorf("helper refused: %s", resp.Error)
	}
	return resp, nil
}
