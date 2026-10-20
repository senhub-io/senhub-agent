package httpcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

// A target that stops answering must not hold a cancelled collection:
// the request is cancelled with the context and the cycle reports the
// interruption, not a target down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{srv.URL},
		"timeout": 30,
	})

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
var _ types.ContextCollector = (*HTTPCheckProbe)(nil)
