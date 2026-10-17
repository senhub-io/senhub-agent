package redis

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A server that accepted the connection and never replies must not hold
// a cancelled collection for the per-command timeout.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := io.Copy(io.Discard, conn); err != nil {
			t.Logf("server read ended: %v", err)
		}
	}()

	p := newTestProbe(t)
	p.cfg.Host = "127.0.0.1"
	p.cfg.Port = ln.Addr().(*net.TCPAddr).Port
	p.cfg.Timeout = 30 * time.Second
	p.instance = net.JoinHostPort(p.cfg.Host, strconv.Itoa(p.cfg.Port))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(50*time.Millisecond, cancel)

	started := time.Now()
	points, err := p.CollectContext(ctx)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CollectContext took %v after its context was cancelled", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if len(points) != 0 {
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none", len(points))
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server goroutine did not exit after client connection was closed")
	}
}

// A cycle cancelled before it starts never reports the target down.
func TestCollectContext_CancelledBeforeDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	p := newTestProbe(t)
	p.cfg.Host = "127.0.0.1"
	p.cfg.Port = port
	p.cfg.Timeout = 30 * time.Second
	p.instance = net.JoinHostPort(p.cfg.Host, strconv.Itoa(p.cfg.Port))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	points, err := p.CollectContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if len(points) != 0 {
		t.Fatalf("a cancelled-before-dial cycle emitted %d datapoints, want none", len(points))
	}
}

// Without a cancellation an unreachable server is still a measurement.
func TestCollect_StillReportsDownWhenNotCancelled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	p := newTestProbe(t)
	p.cfg.Host = "127.0.0.1"
	p.cfg.Port = port
	p.cfg.Timeout = 5 * time.Second
	p.instance = net.JoinHostPort(p.cfg.Host, strconv.Itoa(p.cfg.Port))

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}

	foundUp0 := false
	for _, pt := range points {
		if pt.Name == "senhub.db.up" && pt.Value == 0 {
			foundUp0 = true
			break
		}
	}
	if !foundUp0 {
		t.Fatalf("Collect() did not emit senhub.db.up=0 for an unreachable target")
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*redisProbe)(nil)
