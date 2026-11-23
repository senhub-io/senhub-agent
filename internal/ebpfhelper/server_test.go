package ebpfhelper

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/ebpfproto"
)

func startServer(t *testing.T, tr *Tracker, dropped func() uint64) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ebs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "e.sock")
	srv := NewServer(tr, Info{HelperVersion: "test", Kernel: "6.8.0", Programs: []string{"p"}, Started: time.Now()}, dropped, zerolog.Nop())
	if err := srv.Listen(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return path
}

func ask(t *testing.T, conn net.Conn, r *bufio.Reader, req ebpfproto.Request) (ebpfproto.Response, error) {
	t.Helper()
	if err := ebpfproto.Write(conn, req); err != nil {
		t.Fatal(err)
	}
	return ebpfproto.ReadResponse(r)
}

func TestServerHelloAndSnapshot(t *testing.T) {
	tr := NewTracker(0, time.Minute)
	tr.Apply(state(1, StateClose, StateSynSent, 0, 5432, 4242, "psql"))
	tr.Apply(state(1, StateSynSent, StateEstablished, 41234, 5432, 0, ""))
	path := startServer(t, tr, func() uint64 { return 7 })

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := ebpfproto.NewReader(conn)

	hello, err := ask(t, conn, r, ebpfproto.Request{V: ebpfproto.Version, Op: ebpfproto.OpHello})
	if err != nil || hello.Helper != "senhub-ebpf" || hello.Kernel != "6.8.0" || !hello.BTF || hello.Dropped != 7 {
		t.Fatalf("%+v %v", hello, err)
	}
	snap, err := ask(t, conn, r, ebpfproto.Request{V: ebpfproto.Version, Op: ebpfproto.OpSnapshot})
	if err != nil || len(snap.Conns) != 1 || snap.Conns[0].PID != 4242 || snap.Dropped != 7 {
		t.Fatalf("%+v %v", snap, err)
	}
}

func TestServerSocketMode(t *testing.T) {
	path := startServer(t, NewTracker(0, 0), nil)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o660 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestServerRefusesUnknownOperationAndVersion(t *testing.T) {
	path := startServer(t, NewTracker(0, 0), nil)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := ebpfproto.NewReader(conn)
	if _, err := ask(t, conn, r, ebpfproto.Request{V: ebpfproto.Version, Op: "dump-everything"}); err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("%v", err)
	}
	// The connection survives an unknown operation.
	if _, err := ask(t, conn, r, ebpfproto.Request{V: ebpfproto.Version, Op: ebpfproto.OpHello}); err != nil {
		t.Fatal(err)
	}

	conn2, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	if _, err := ask(t, conn2, ebpfproto.NewReader(conn2), ebpfproto.Request{V: 9, Op: ebpfproto.OpHello}); err == nil {
		t.Fatal("another version must be refused")
	}
}

func TestServerReplacesStaleSocketAndLimitsClients(t *testing.T) {
	dir, err := os.MkdirTemp("", "ebs")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "e.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(NewTracker(0, 0), Info{Started: time.Now()}, nil, zerolog.Nop())
	if err := srv.Listen(path); err != nil {
		t.Fatalf("a stale file must be replaced: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	var held []net.Conn
	for i := 0; i < maxClients; i++ {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	time.Sleep(100 * time.Millisecond)
	extra, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := ebpfproto.ReadResponse(ebpfproto.NewReader(extra)); err == nil || !strings.Contains(err.Error(), "too many clients") {
		t.Fatalf("a client over the limit must be told so: %v", err)
	}
}
