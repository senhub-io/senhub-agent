package solr

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

	p, err := NewSolrProbe(map[string]interface{}{"endpoint": srv.URL, "timeout": 30}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
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
var _ types.ContextCollector = (*SolrProbe)(nil)

// A cluster lookup cut short by the end of the collection says nothing about
// the node: the host:port fallback pinned from it would stick for good.
func TestTryPin_InterruptedLookupPinsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	src := newSolrEntitySource(srv.URL, "", newTestHTTPClient())
	src.setReachable(true, "8.11.2")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src.tryPinClusterIDOrHostPort(ctx)

	if _, ok := src.Observe(); ok {
		t.Fatal("an interrupted cluster lookup pinned an identity")
	}
}
