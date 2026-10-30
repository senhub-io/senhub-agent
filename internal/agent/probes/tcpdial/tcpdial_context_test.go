package tcpdial

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// The production dial honours the context: a cancelled one fails at once
// with the cancellation, even against a listener that would accept.
func TestDialOnce_HonoursCancelledContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	p := newTestProbe(t, ln.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := p.dialOnce(ctx, ln.Addr().String())
	if res.up {
		t.Fatal("a dial under a cancelled context reported the target up")
	}
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", res.err)
	}
}

// A dial that hangs must not hold a cancelled collection, and the
// interrupted cycle must not report its targets down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	p := newTestProbe(t, "192.0.2.1:9")
	p.dial = func(ctx context.Context, target string) dialResult {
		<-ctx.Done()
		return dialResult{target: target, err: ctx.Err()}
	}

	ctx, cancel := context.WithCancel(context.Background())
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
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none (no false up=0)", len(points))
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*TCPDialProbe)(nil)
