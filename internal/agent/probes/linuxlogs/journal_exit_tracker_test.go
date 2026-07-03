package linuxlogs

import (
	"errors"
	"testing"
)

// TestExitTracker_UnexpectedDeath pins the core of the fix: a subprocess
// exit that was not preceded by a stop() request (markStopping) is
// classified as unexpected. The pre-fix probe had no such classification
// at all — journalctl death was invisible — so this behaviour is the
// mechanism that lets Collect() report the probe unhealthy.
func TestExitTracker_UnexpectedDeath(t *testing.T) {
	tr := newExitTracker()

	if tr.exitedUnexpectedly() {
		t.Fatal("fresh tracker must not report an exit")
	}

	exitErr := errors.New("signal: killed")
	tr.recordExit(exitErr)

	if !tr.exitedUnexpectedly() {
		t.Error("exit without markStopping must be flagged unexpected")
	}
	if !errors.Is(tr.err(), exitErr) {
		t.Errorf("err() = %v, want %v", tr.err(), exitErr)
	}
	select {
	case <-tr.waitCh():
	default:
		t.Error("waitCh must be closed after recordExit")
	}
}

// TestExitTracker_CleanStop verifies the expected-shutdown path: stop()
// calls markStopping before signalling, so the resulting exit is not
// flagged unexpected and the probe stays healthy through a deliberate
// shutdown.
func TestExitTracker_CleanStop(t *testing.T) {
	tr := newExitTracker()

	tr.markStopping()
	tr.recordExit(errors.New("signal: terminated"))

	if tr.exitedUnexpectedly() {
		t.Error("exit after markStopping must not be flagged unexpected")
	}
	select {
	case <-tr.waitCh():
	default:
		t.Error("waitCh must be closed after recordExit")
	}
}

// TestExitTracker_RecordExitOnce guards the idempotency the monitor
// goroutine relies on: only the first exit is retained and waitCh is
// closed exactly once (a second close would panic).
func TestExitTracker_RecordExitOnce(t *testing.T) {
	tr := newExitTracker()

	first := errors.New("first")
	tr.recordExit(first)
	tr.recordExit(errors.New("second"))

	if !errors.Is(tr.err(), first) {
		t.Errorf("err() = %v, want first exit %v", tr.err(), first)
	}
}
