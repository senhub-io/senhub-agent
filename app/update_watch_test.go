package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time        { return c.t }
func (c *fakeClock) sleep(d time.Duration) { c.t = c.t.Add(d) }

func newTestWatch(clock *fakeClock, w *updateWatch) {
	w.wait = 10 * time.Second
	w.settle = 3 * time.Second
	w.poll = time.Second
	w.now = clock.now
	w.sleep = clock.sleep
}

func TestRunServiceStaysOnBinary(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	restored := false
	w := updateWatch{
		running: func() (string, bool) { return "42", true },
		exePath: func(pid string) (string, error) { return `C:\agent\senhub-agent.exe`, nil },
		restore: func() error { restored = true; return nil },
		start:   func() (string, error) { return "", nil },
		binary:  `c:\AGENT\senhub-agent.exe`,
	}
	newTestWatch(clock, &w)
	outcome, err := w.run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != "running" {
		t.Fatalf("outcome = %q, want %q", outcome, "running")
	}
	if restored {
		t.Fatal("restore must not be called when the service stays up")
	}
}

func TestRunServiceNeverStarts(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	restores, starts := 0, 0
	w := updateWatch{
		running: func() (string, bool) { return "", false },
		exePath: func(pid string) (string, error) { return "", nil },
		restore: func() error { restores++; return nil },
		start:   func() (string, error) { starts++; return "ok", nil },
		binary:  `C:\agent\senhub-agent.exe`,
	}
	newTestWatch(clock, &w)
	outcome, err := w.run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != "restored" {
		t.Fatalf("outcome = %q, want %q", outcome, "restored")
	}
	if restores != 1 || starts != 1 {
		t.Fatalf("restore=%d start=%d, want 1 and 1", restores, starts)
	}
}

func TestRunPidChangesBeforeSettle(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	calls := 0
	w := updateWatch{
		running: func() (string, bool) {
			calls++
			if calls <= 1 {
				return "1", true // first sighting, then the PID changes
			}
			if calls == 2 {
				return "2", true // different PID during settle
			}
			return "", false // never comes back
		},
		exePath: func(pid string) (string, error) { return `C:\agent\senhub-agent.exe`, nil },
		restore: func() error { return nil },
		start:   func() (string, error) { return "ok", nil },
		binary:  `C:\agent\senhub-agent.exe`,
	}
	newTestWatch(clock, &w)
	outcome, err := w.run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != "restored" {
		t.Fatalf("outcome = %q, want %q", outcome, "restored")
	}
}

func TestRunRestoreFails(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	started := false
	w := updateWatch{
		running: func() (string, bool) { return "", false },
		exePath: func(pid string) (string, error) { return "", nil },
		restore: func() error { return errors.New("disk full") },
		start:   func() (string, error) { started = true; return "", nil },
		binary:  `C:\agent\senhub-agent.exe`,
	}
	newTestWatch(clock, &w)
	outcome, err := w.run()
	if err == nil {
		t.Fatal("expected an error when restore fails")
	}
	if !strings.Contains(err.Error(), "restoring the previous binary") {
		t.Fatalf("error = %v, want it wrapped with %q", err, "restoring the previous binary")
	}
	if outcome != "" {
		t.Fatalf("outcome = %q, want empty on restore failure", outcome)
	}
	if started {
		t.Fatal("start must not be called when restore fails")
	}
}
