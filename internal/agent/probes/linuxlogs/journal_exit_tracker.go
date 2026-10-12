package linuxlogs

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// shutdownGrace is how long a SIGTERM exit waits for the agent's own stop
// request. The service manager signals the whole control group at once, so
// journalctl can be gone a few milliseconds before the agent's handler runs.
const shutdownGrace = 2 * time.Second

// diedOfSIGTERM reports whether err is a process killed by SIGTERM.
func diedOfSIGTERM(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGTERM
}

// expectedAtShutdown reports whether an exit is the one the service manager
// causes when it stops the agent: a SIGTERM death while the agent is, or
// within the grace period becomes, shutting down. A SIGTERM from anyone else
// at any other time stays an unexpected death.
func expectedAtShutdown(err error, shuttingDown func() bool, grace time.Duration) bool {
	if !diedOfSIGTERM(err) {
		return false
	}
	deadline := time.Now().Add(grace)
	for {
		if shuttingDown() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

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
