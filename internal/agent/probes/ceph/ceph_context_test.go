package ceph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A target that stops answering must not hold a cancelled collection,
// and an interrupted cycle must not be reported as a target down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	p, err := NewCephProbe(map[string]interface{}{"endpoint": srv.URL, "username": "u", "password": "p", "timeout": 30}, newTestLogger())
	if err != nil {
		t.Fatalf("NewCephProbe: %v", err)
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
var _ types.ContextCollector = (*CephProbe)(nil)

// A fsid lookup cut short by the end of the collection says nothing about
// the cluster: pinning the host-based fallback from it would fix the wrong
// identity for good.
func TestPinID_InterruptedLookupPinsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := newCephEntitySource(
		"",
		"https://ceph.example.com:8443",
		func(ctx context.Context) (string, error) {
			cancel()
			return "", ctx.Err()
		},
		stubHostID("h-001"),
	)

	src.pinID(ctx)

	if _, ok := src.Observe(); ok {
		t.Fatal("an interrupted fsid lookup pinned an identity")
	}
}
