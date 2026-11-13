package varnish

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A varnishstat that hangs must not hold a cancelled collection, and an
// interrupted cycle must not be reported as Varnish down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell to stand in for varnishstat")
	}
	script := filepath.Join(t.TempDir(), "varnishstat")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatalf("writing fake varnishstat: %v", err)
	}

	p, err := NewVarnishProbe(map[string]interface{}{"varnishstat_path": script}, nil)
	if err != nil {
		t.Fatalf("NewVarnishProbe: %v", err)
	}
	cc, ok := any(p).(types.ContextCollector)
	if !ok {
		t.Fatalf("%T does not implement types.ContextCollector", p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
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
var _ types.ContextCollector = (*VarnishProbe)(nil)
