package auto_update

import "sync/atomic"

// paused gates the periodic update check; it is process-local and does
// not survive a restart.
var paused atomic.Bool

// resumeHook runs asynchronously when the pause is lifted, so an extension
// resuming the agent triggers an immediate check instead of waiting for the
// next scheduled tick.
var resumeHook atomic.Pointer[func()]

// SetPaused suspends (true) or resumes (false) the periodic update check.
func SetPaused(p bool) {
	was := paused.Swap(p)
	if was && !p {
		if hook := resumeHook.Load(); hook != nil {
			go (*hook)()
		}
	}
}

// Paused reports whether the periodic update check is suspended.
func Paused() bool { return paused.Load() }

// setResumeHook installs the hook run on resume; nil clears it.
func setResumeHook(f func()) {
	if f == nil {
		resumeHook.Store(nil)
		return
	}
	resumeHook.Store(&f)
}
