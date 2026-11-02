package auto_update

import "sync/atomic"

// paused gates the periodic update check; it is process-local and does
// not survive a restart.
var paused atomic.Bool

// SetPaused suspends (true) or resumes (false) the periodic update check.
func SetPaused(p bool) { paused.Store(p) }

// Paused reports whether the periodic update check is suspended.
func Paused() bool { return paused.Load() }
