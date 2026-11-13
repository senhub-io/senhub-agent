package icmpcheck

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A target that never answers must not hold a cancelled collection, and
// the interrupted round must not be reported as every target down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{"192.0.2.1", "192.0.2.2"},
	})
	p.ping = func(ctx context.Context, target string) pingResult {
		<-ctx.Done()
		return pingResult{target: target, err: ctx.Err(), lossRatio: 1}
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
	if _, ok := p.entitySrc.Observe(); ok {
		t.Fatal("an interrupted cycle published an entity observation")
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*ICMPCheckProbe)(nil)
