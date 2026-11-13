package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// updateWatch decides, after a self-update exit, whether the service came up
// on the new binary or must be rolled back to the previous one. All side
// effects are injected so the decision logic stays pure and testable.
type updateWatch struct {
	running func() (pid string, ok bool)     // service is running, with its PID
	exePath func(pid string) (string, error) // binary path of that PID
	restore func() error                     // put the .previous binary back in place
	start   func() (string, error)           // start the service even if stopped
	binary  string
	wait    time.Duration // total observation window
	settle  time.Duration // how long the same PID must stay up
	poll    time.Duration // polling interval
	now     func() time.Time
	sleep   func(time.Duration)
}

func (w updateWatch) run() (outcome string, err error) {
	deadline := w.now().Add(w.wait)
	for {
		if pid, ok := w.running(); ok {
			if exe, exeErr := w.exePath(pid); exeErr == nil && sameBinary(exe, w.binary) && w.stable(pid) {
				// The service is up on the expected binary; even if it is the
				// old one, the startup guard already rolled back by itself.
				return "running", nil
			}
		}
		if !w.now().Before(deadline) {
			break
		}
		w.sleep(w.poll)
	}
	if err := w.restore(); err != nil {
		return "", fmt.Errorf("restoring the previous binary: %w", err)
	}
	if _, err := w.start(); err != nil {
		return "restored", fmt.Errorf("starting the restored version: %w", err)
	}
	return "restored", nil
}

// stable reports whether pid keeps running for the whole settle window,
// even past the observation deadline: a start is only good once it held.
func (w updateWatch) stable(pid string) bool {
	end := w.now().Add(w.settle)
	for w.now().Before(end) {
		w.sleep(w.poll)
		if cur, ok := w.running(); !ok || cur != pid {
			return false
		}
	}
	return true
}

func sameBinary(a, b string) bool {
	return strings.EqualFold(cleanWatchPath(a), cleanWatchPath(b))
}

func cleanWatchPath(p string) string {
	return strings.TrimPrefix(filepath.Clean(p), `\\?\`)
}
