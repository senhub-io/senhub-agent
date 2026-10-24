package http

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const (
	// defaultFirstDataWindow is how long after the start a request may
	// wait for a probe's first data. Past it, a probe with no data
	// answers at once, so a probe that never collects does not slow its
	// sensor for good.
	defaultFirstDataWindow = 2 * time.Minute
	// defaultFirstDataWait is the longest a request waits; it stays
	// below PRTG's default 60 s request timeout.
	defaultFirstDataWait = 10 * time.Second
)

// waitForFirstData holds a pull request for a running probe that has no
// data yet until its first points land, its first collection finishes,
// the wait elapses or the client goes. It returns at once for a probe the
// agent does not run, one whose last cycle failed, one that already has
// data, an event-driven one (no first collection to wait for), one whose
// first collection already finished, or once the start window is over. The caller reads the cache afterwards whatever the outcome.
func (h *HTTPSyncStrategy) waitForFirstData(ctx context.Context, probeName string) {
	window, wait := h.firstDataWindow, h.firstDataWait
	if window <= 0 {
		window = defaultFirstDataWindow
	}
	if wait <= 0 {
		wait = defaultFirstDataWait
	}
	if time.Since(h.startTime) >= window {
		return
	}
	state, running := agentstate.ProbeRunStateByName(probeName)
	if !running || state.Health == "failed" || state.EventDriven || !state.LastCycle.IsZero() {
		return
	}
	cycled, _ := agentstate.FirstCycleSignalByName(probeName)
	ready := h.cache.FirstDataSignal(probeName)
	select {
	case <-ready:
		return
	default:
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	got := false
	select {
	case <-ready:
		got = true
	case <-cycled:
	case <-timer.C:
	case <-ctx.Done():
	}

	if _, logged := h.firstDataWaitLogged.LoadOrStore(probeName, struct{}{}); !logged {
		h.logger.Debug().
			Str("probe", probeName).
			Bool("got_data", got).
			Dur("waited_max", wait).
			Msg("Pull request waited for the probe's first data")
	}
}
