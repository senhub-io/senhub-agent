package linuxlogs

import "sync"

// exitTracker records the exit of the journalctl subprocess and tells an
// expected shutdown (we asked for it) apart from an unexpected death (journald
// restart, OOM-kill, crash). It is platform-independent so the classification
// can be unit-tested without spawning journalctl.
//
// Ordering contract: stop() calls markStopping before signalling the process,
// so the exit the signal causes is classified as expected. A recordExit that
// arrives with no prior markStopping is an unexpected death.
type exitTracker struct {
	mu         sync.Mutex
	stopping   bool
	exited     bool
	unexpected bool
	exitErr    error
	done       chan struct{}
}

func newExitTracker() *exitTracker {
	return &exitTracker{done: make(chan struct{})}
}

// markStopping declares that an exit is now expected — a deliberate stop().
func (t *exitTracker) markStopping() {
	t.mu.Lock()
	t.stopping = true
	t.mu.Unlock()
}

// recordExit registers the subprocess exit exactly once. An exit that was not
// preceded by markStopping is flagged unexpected. Closing done unblocks stop().
func (t *exitTracker) recordExit(err error) {
	t.mu.Lock()
	if t.exited {
		t.mu.Unlock()
		return
	}
	t.exited = true
	t.exitErr = err
	if !t.stopping {
		t.unexpected = true
	}
	t.mu.Unlock()
	close(t.done)
}

// exitedUnexpectedly reports whether journalctl died without a stop() request.
func (t *exitTracker) exitedUnexpectedly() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.unexpected
}

// err returns the subprocess exit error (nil until it exits).
func (t *exitTracker) err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitErr
}

// waitCh is closed when the subprocess exit has been recorded.
func (t *exitTracker) waitCh() <-chan struct{} {
	return t.done
}
