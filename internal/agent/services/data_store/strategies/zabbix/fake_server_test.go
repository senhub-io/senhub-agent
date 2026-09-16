package zabbix

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"net"
	"sync"
	"testing"
)

// fakeServer answers like a Zabbix server: it records every request and
// replies with the item list and the processing summary a test asked for.
type fakeServer struct {
	t        *testing.T
	ln       net.Listener
	mu       sync.Mutex
	requests []map[string]interface{}
	items    []map[string]interface{}
	// refuse names request types answered with {"response":"failed"};
	// refuseInfo gives the info text of such a refusal.
	refuse     map[string]bool
	refuseInfo map[string]string
	// compressReplies makes the server send zlib-compressed frames.
	compressReplies bool
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{t: t, ln: ln, refuse: map[string]bool{}, refuseInfo: map[string]string{}}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeServer) addr() string { return s.ln.Addr().String() }

func (s *fakeServer) setItems(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
	for i, k := range keys {
		s.items = append(s.items, map[string]interface{}{"key": k, "itemid": 1000 + i, "delay": "60s"})
	}
}

func (s *fakeServer) requestsOf(kind string) []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]interface{}
	for _, r := range s.requests {
		if r["request"] == kind {
			out = append(out, r)
		}
	}
	return out
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeServer) handle(conn net.Conn) {
	defer conn.Close()
	payload, err := readFrame(conn)
	if err != nil {
		return
	}
	var req map[string]interface{}
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	kind, _ := req["request"].(string)
	refused := s.refuse[kind] || s.refuseInfo[kind] != ""
	info := s.refuseInfo[kind]
	if info == "" {
		info = "refused by test"
	}
	items := s.items
	compress := s.compressReplies
	s.mu.Unlock()

	var reply map[string]interface{}
	switch {
	case refused:
		reply = map[string]interface{}{"response": "failed", "info": info}
	case kind == "active checks":
		reply = map[string]interface{}{"response": "success", "data": items}
	case kind == "agent data":
		data, _ := req["data"].([]interface{})
		reply = map[string]interface{}{"response": "success", "info": summary(len(data))}
	case kind == "active check heartbeat":
		// A real server closes the connection without answering.
		return
	default:
		reply = map[string]interface{}{"response": "failed", "info": "unknown request"}
	}
	body, _ := json.Marshal(reply)
	if compress {
		writeCompressedFrame(conn, body)
		return
	}
	_ = writeFrame(conn, body)
}

func summary(n int) string {
	return "processed: " + itoa(n) + "; failed: 0; total: " + itoa(n) + "; seconds spent: 0.000100"
}

func itoa(n int) string {
	return string(rune('0' + n%10)) // tests send fewer than ten values
}

func writeCompressedFrame(conn net.Conn, body []byte) {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(body)
	_ = w.Close()
	frame := []byte(frameMagic)
	frame = append(frame, flagProtocol|flagCompressed)
	frame = binary.LittleEndian.AppendUint32(frame, uint32(z.Len()))
	frame = binary.LittleEndian.AppendUint32(frame, uint32(len(body)))
	frame = append(frame, z.Bytes()...)
	_, _ = conn.Write(frame)
}
