package dnslatency

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// A server that receives the query and never answers must not hold a
// cancelled collection, and an interrupted cycle must not be reported as
// a resolver down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.LocalAddr().String()

	p, err := NewDNSLatencyProbe(map[string]interface{}{"names": []interface{}{"host.example.test"}, "resolvers": []interface{}{addr}, "timeout": 30}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewDNSLatencyProbe: %v", err)
	}
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
var _ types.ContextCollector = (*DNSLatencyProbe)(nil)
