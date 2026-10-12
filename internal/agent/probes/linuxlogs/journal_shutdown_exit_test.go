//go:build !windows

package linuxlogs

import (
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func exitOf(t *testing.T, script string) error {
	t.Helper()
	return exec.Command("sh", "-c", script).Run()
}

// systemd stops the cgroup with SIGTERM before the agent's handler may have
// run; the exit is expected once the agent says it is stopping (#591).
func TestExpectedAtShutdown_SIGTERMWhileStopping(t *testing.T) {
	err := exitOf(t, "kill -TERM $$")
	if !diedOfSIGTERM(err) {
		t.Fatalf("fixture did not die of SIGTERM: %v", err)
	}
	if !expectedAtShutdown(err, func() bool { return true }, time.Second) {
		t.Error("SIGTERM during shutdown must be an expected exit")
	}
}

func TestExpectedAtShutdown_StopRequestArrivesWithinGrace(t *testing.T) {
	err := exitOf(t, "kill -TERM $$")
	var stopping atomic.Bool
	time.AfterFunc(60*time.Millisecond, func() { stopping.Store(true) })
	if !expectedAtShutdown(err, stopping.Load, 2*time.Second) {
		t.Error("a stop request landing just after the SIGTERM must still count")
	}
}

func TestExpectedAtShutdown_SIGTERMWhileRunningStaysUnexpected(t *testing.T) {
	err := exitOf(t, "kill -TERM $$")
	if expectedAtShutdown(err, func() bool { return false }, 50*time.Millisecond) {
		t.Error("a SIGTERM with no stop request is a death to report")
	}
}

func TestExpectedAtShutdown_OtherExitsStayUnexpected(t *testing.T) {
	for name, script := range map[string]string{
		"killed":       "kill -KILL $$",
		"exit status":  "exit 3",
		"clean return": "exit 0",
	} {
		if expectedAtShutdown(exitOf(t, script), func() bool { return true }, 50*time.Millisecond) {
			t.Errorf("%s: only a SIGTERM exit is the service manager's stop", name)
		}
	}
}

func TestExitTracker_SIGTERMAtShutdownIsNotFlaggedUnexpected(t *testing.T) {
	tr := newExitTracker()
	err := exitOf(t, "kill -TERM $$")
	if expectedAtShutdown(err, func() bool { return true }, time.Second) {
		tr.markStopping()
	}
	tr.recordExit(err)
	if tr.exitedUnexpectedly() {
		t.Error("the exit at shutdown was flagged unexpected, which logs an ERR on every stop")
	}
}
