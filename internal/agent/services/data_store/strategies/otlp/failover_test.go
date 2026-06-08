package otlp

import (
	"errors"
	"testing"
	"time"
)

func TestFailoverCore_PrefersPrimaryFallsBackAndReturns(t *testing.T) {
	core := newFailoverCore([]string{"primary:4317", "standby:4317"}, 50*time.Millisecond, testModuleLogger(t))

	primaryUp := false
	attempt := func(i int) error {
		if i == 0 && !primaryUp {
			return errors.New("primary down")
		}
		return nil
	}

	// Call 1: primary fails → standby serves.
	if err := core.do(attempt); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if got := core.active.Load(); got != 1 {
		t.Errorf("active=%d, want 1 (failed over to standby)", got)
	}

	// Call 2 immediately: primary is in cooldown → skipped, standby serves.
	if err := core.do(attempt); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if got := core.active.Load(); got != 1 {
		t.Errorf("active=%d, want 1 (primary still cooling down)", got)
	}

	// Primary recovers and the cooldown elapses.
	primaryUp = true
	time.Sleep(70 * time.Millisecond)

	// Call 3: primary is tried first again → automatic return to primary.
	if err := core.do(attempt); err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if got := core.active.Load(); got != 0 {
		t.Errorf("active=%d, want 0 (returned to primary after recovery)", got)
	}
}

func TestFailoverCore_AllDownReturnsError(t *testing.T) {
	core := newFailoverCore([]string{"a:4317", "b:4317"}, time.Second, testModuleLogger(t))
	err := core.do(func(int) error { return errors.New("down") })
	if err == nil {
		t.Error("expected an error when every endpoint is down")
	}
}

func TestFailoverCore_AllInCooldownStillTries(t *testing.T) {
	// Long cooldown: after both fail once, a later call with everyone
	// healthy must still try them (pass 2) rather than drop.
	core := newFailoverCore([]string{"a:4317", "b:4317"}, time.Hour, testModuleLogger(t))
	_ = core.do(func(int) error { return errors.New("down") }) // both cool down
	if err := core.do(func(int) error { return nil }); err != nil {
		t.Fatalf("expected pass-2 to retry cooled-down endpoints, got %v", err)
	}
	if got := core.active.Load(); got != 0 {
		t.Errorf("active=%d, want 0", got)
	}
}
