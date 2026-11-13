package mongodb

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A server that accepts the connection and never answers must not hold a
// cancelled collection, and an interrupted cycle must not be reported as
// a server down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	p, err := NewMongoDBProbe(map[string]interface{}{
		"uri":     "mongodb://" + ln.Addr().String(),
		"timeout": 30,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewMongoDBProbe: %v", err)
	}
	if err := p.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	defer func() { _ = p.OnShutdown(context.Background()) }()
	cc, ok := any(p).(types.ContextCollector)
	if !ok {
		t.Fatalf("%T does not implement types.ContextCollector", p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	points, err := cc.CollectContext(ctx)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CollectContext took %v after its context was cancelled", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if len(points) != 0 {
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none (no false up=0)", len(points))
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*mongoDBProbe)(nil)
