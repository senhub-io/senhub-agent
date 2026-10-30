package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
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
	// writeDeadlineMargin is what a request's write deadline keeps beyond
	// the wait, for the response itself.
	writeDeadlineMargin = 2 * time.Second
)

// waitForFirstData holds a pull request for a probe that has no data yet
// until its first points land, its first collection finishes, the wait
// elapses or the client goes. The probe is either running, or configured
// and enabled but not registered yet (the sensor starts after the
// outputs, so a reading in the first seconds precedes it); in the second
// case the wait goes on until the probe registers, then applies the same
// rules. It returns at once for a name the agent neither runs nor
// configures, one whose last cycle failed, one that already has data, an
// event-driven one (no first collection to wait for), one whose first
// collection already finished, or once the start window is over. The
// caller reads the cache afterwards whatever the outcome.
func (h *HTTPSyncStrategy) waitForFirstData(ctx context.Context, w http.ResponseWriter, probeName string) {
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

	registered := agentstate.ActiveProbeNamesChanged()
	state, running := agentstate.ProbeRunStateByName(probeName)
	if !running && !h.probeConfigured(probeName) {
		return
	}
	if running && !worthWaiting(state) {
		return
	}
	ready := h.cache.FirstDataSignal(probeName)
	select {
	case <-ready:
		return
	default:
	}

	wait = h.extendWriteDeadline(w, wait)
	timer := time.NewTimer(wait)
	defer timer.Stop()

	for !running {
		select {
		case <-registered:
		case <-ready:
			h.logFirstDataWait(probeName, true, wait)
			return
		case <-timer.C:
			h.logFirstDataWait(probeName, false, wait)
			return
		case <-ctx.Done():
			h.logFirstDataWait(probeName, false, wait)
			return
		}
		registered = agentstate.ActiveProbeNamesChanged()
		state, running = agentstate.ProbeRunStateByName(probeName)
		if running && !worthWaiting(state) {
			return
		}
	}

	cycled, _ := agentstate.FirstCycleSignalByName(probeName)
	got := false
	select {
	case <-ready:
		got = true
	case <-cycled:
	case <-timer.C:
	case <-ctx.Done():
	}
	h.logFirstDataWait(probeName, got, wait)
}

func worthWaiting(state agentstate.ProbeRunState) bool {
	return state.Health != "failed" && !state.EventDriven && state.LastCycle.IsZero()
}

// probeConfigured reports whether the current configuration holds an
// enabled probe of that name, running or not.
func (h *HTTPSyncStrategy) probeConfigured(probeName string) bool {
	provider, ok := h.agentConfig.(interface {
		GetConfiguration() configuration.ConfigurationData
	})
	if !ok {
		return false
	}
	for _, p := range provider.GetConfiguration().Probes {
		if p.IsEnabled() && strings.EqualFold(p.Name, probeName) {
			return true
		}
	}
	return false
}

// extendWriteDeadline moves this response's write deadline past the wait,
// since the server-wide write timeout is about the length of the wait and
// would close the connection before the answer is written. When the
// writer cannot move its deadline, the wait is cut to fit under the
// server's timeout instead. It returns the wait to use.
func (h *HTTPSyncStrategy) extendWriteDeadline(w http.ResponseWriter, wait time.Duration) time.Duration {
	err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(wait + writeDeadlineMargin))
	if err == nil {
		return wait
	}
	if !errors.Is(err, http.ErrNotSupported) {
		h.logger.Debug().Err(err).Msg("Could not extend the response write deadline")
	}
	if limit := serverWriteTimeout - writeDeadlineMargin; wait > limit {
		return limit
	}
	return wait
}

func (h *HTTPSyncStrategy) logFirstDataWait(probeName string, got bool, wait time.Duration) {
	if _, logged := h.firstDataWaitLogged.LoadOrStore(probeName, struct{}{}); !logged {
		h.logger.Debug().
			Str("probe", probeName).
			Bool("got_data", got).
			Dur("waited_max", wait).
			Msg("Pull request waited for the probe's first data")
	}
}
