package auto_update

import (
	"sync"
	"sync/atomic"
)

// checkBinaryReplaceable is a package-level indirection so tests can stub
// the binary-replaceability probe.
var checkBinaryReplaceable = CheckBinaryReplaceable

// enabledState tracks the last known auto-update enabled flag so each
// transition is logged exactly once. It is added to autoUpdate as the
// field `enabled enabledState`.
type enabledState struct {
	known atomic.Bool
	on    atomic.Bool
}

// noteEnabled records the current enabled flag and logs only actual
// transitions; the first observation is a baseline, not a transition.
func (a *autoUpdate) noteEnabled(enabled bool) {
	if !a.enabled.known.Swap(true) {
		a.enabled.on.Store(enabled)
		return
	}
	if a.enabled.on.Swap(enabled) == enabled {
		return
	}
	if enabled {
		a.logger.Info().Bool("enabled", true).Msg("Auto-update enabled")
		if warn := checkBinaryReplaceable(); warn != "" {
			a.logger.Warn().Msg(warn)
		}
	} else {
		a.logger.Info().Msg("Auto-update disabled")
	}
}

// selfReplaceState tracks self-replaceability the same way enabledState
// tracks the enabled flag: the first observation is a baseline, only an
// actual change is notified.
type selfReplaceState struct {
	// mu makes the compare-and-record one step: the scheduler and the
	// directive hook can call noteSelfReplace together, and two
	// interleaved swaps could lose a change.
	mu    sync.Mutex
	known bool
	on    bool
}

// noteSelfReplace records the current self-replaceability; a change means a
// server that drives the update must be told the agent can (or can no
// longer) install a new version by itself.
func (a *autoUpdate) noteSelfReplace() {
	on := CanReplaceSelf()
	a.selfReplace.mu.Lock()
	changed := a.selfReplace.known && a.selfReplace.on != on
	a.selfReplace.known, a.selfReplace.on = true, on
	a.selfReplace.mu.Unlock()
	if changed {
		notifyStatusListeners()
	}
}

// checkAfterResume is the resume hook: it runs an immediate update check
// when an extension lifts the pause.
func (a *autoUpdate) checkAfterResume() {
	a.logger.Info().Msg("Auto-update resumed by an extension; checking now")
	if err := a.PeriodicalCheckForUpdate(); err != nil {
		a.logger.Error().Err(err).Msg("Failed to check for update after resume")
	}
}
