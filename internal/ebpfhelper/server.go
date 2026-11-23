package ebpfhelper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/ebpfproto"
)

const (
	maxClients      = 4
	requestDeadline = 5 * time.Second
	writeDeadline   = 20 * time.Second
)

// Info is what the helper tells a client about itself.
type Info struct {
	HelperVersion string
	Kernel        string
	Programs      []string
	Started       time.Time
}

// Server answers the agent's requests on a unix socket.
type Server struct {
	tracker *Tracker
	info    Info
	dropped func() uint64
	log     zerolog.Logger
	now     func() time.Time

	mu sync.Mutex
	ln net.Listener
}

// NewServer returns a Server over tracker. dropped reports the kernel's own
// count of events it could not queue; it may be nil.
func NewServer(tracker *Tracker, info Info, dropped func() uint64, log zerolog.Logger) *Server {
	if dropped == nil {
		dropped = func() uint64 { return 0 }
	}
	return &Server{tracker: tracker, info: info, dropped: dropped, log: log, now: time.Now}
}

// Listen binds the socket at path with mode 0660. The group is the one the
// process runs as, which the unit sets to the agent's group. A stale socket
// file from a previous run is replaced.
func (s *Server) Listen(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing stale socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = ln.Close()
		return fmt.Errorf("setting mode of %s: %w", path, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	return nil
}

// Serve accepts clients until ctx is done or the listener is closed.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return errors.New("server is not listening")
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	slots := make(chan struct{}, maxClients)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accepting client: %w", err)
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.SetWriteDeadline(s.now().Add(requestDeadline))
			_ = ebpfproto.Write(conn, ebpfproto.Response{V: ebpfproto.Version, Error: "too many clients"})
			_ = conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			s.handle(conn)
		}()
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	r := ebpfproto.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(s.now().Add(requestDeadline))
		req, err := ebpfproto.ReadRequest(r)
		if err != nil {
			if errors.Is(err, ebpfproto.ErrVersion) {
				_ = ebpfproto.Write(conn, ebpfproto.Response{V: ebpfproto.Version, Error: err.Error()})
			}
			return
		}
		_ = conn.SetWriteDeadline(s.now().Add(writeDeadline))
		if err := ebpfproto.Write(conn, s.answer(req)); err != nil {
			s.log.Debug().Err(err).Msg("writing response")
			return
		}
	}
}

func (s *Server) answer(req ebpfproto.Request) ebpfproto.Response {
	resp := ebpfproto.Response{V: ebpfproto.Version, OK: true, Op: req.Op}
	switch req.Op {
	case ebpfproto.OpHello:
		resp.Helper = "senhub-ebpf"
		resp.HelperVersion = s.info.HelperVersion
		resp.Kernel = s.info.Kernel
		resp.BTF = true
		resp.Programs = s.info.Programs
		resp.UptimeSeconds = int64(s.now().Sub(s.info.Started).Seconds())
		resp.Dropped = s.dropped()
		resp.Tracked = s.tracker.Len()
	case ebpfproto.OpSnapshot:
		resp.Conns, resp.Evicted, resp.Tracked = s.tracker.Snapshot()
		resp.Dropped = s.dropped()
	default:
		return ebpfproto.Response{V: ebpfproto.Version, Op: req.Op, Error: "unknown operation"}
	}
	return resp
}
