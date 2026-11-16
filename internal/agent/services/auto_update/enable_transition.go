package auto_update

import "sync/atomic"

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

// checkAfterResume is the resume hook: it runs an immediate update check
// when an extension lifts the pause.
func (a *autoUpdate) checkAfterResume() {
	a.logger.Info().Msg("Auto-update resumed by an extension; checking now")
	if err := a.PeriodicalCheckForUpdate(); err != nil {
		a.logger.Error().Err(err).Msg("Failed to check for update after resume")
	}
}
