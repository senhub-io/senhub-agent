package app

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func fakeRestarter(pids []string, exe string) (*serviceRestarter, *int) {
	restarts := 0
	i := 0
	r := &serviceRestarter{
		installed: func() bool { return true },
		isActive:  func() bool { return true },
		mainPID: func() string {
			p := pids[min(i, len(pids)-1)]
			i++
			return p
		},
		restart: func() error { restarts++; return nil },
		exePath: func(string) (string, error) { return exe, nil },
		version: func(string) string { return "Version: 0.6.1 (commit: abc)\n" },
		wait:    time.Second,
		poll:    time.Millisecond,
	}
	return r, &restarts
}

func TestServiceRestarter_RunsTheNewBinary(t *testing.T) {
	r, restarts := fakeRestarter([]string{"100", "100", "200"}, "/usr/local/bin/senhub-agent")
	line, err := r.run()
	if err != nil || *restarts != 1 {
		t.Fatalf("got %q, %v, %d restarts", line, err, *restarts)
	}
	if !strings.Contains(line, "0.6.1") {
		t.Errorf("the line must say which version now runs: %q", line)
	}
}

func TestServiceRestarter_RefusesADeletedBinary(t *testing.T) {
	r, _ := fakeRestarter([]string{"100", "200"}, "/usr/local/bin/.senhub-agent.old (deleted)")
	if _, err := r.run(); err == nil {
		t.Fatal("a service still running the deleted binary must fail the update")
	}
}

func TestServiceRestarter_NothingToRestart(t *testing.T) {
	r, restarts := fakeRestarter([]string{"100"}, "x")
	r.isActive = func() bool { return false }
	if line, err := r.run(); line != "" || err != nil || *restarts != 0 {
		t.Errorf("a stopped service is left alone, got %q, %v, %d", line, err, *restarts)
	}
	r.installed = func() bool { return false }
	if line, err := r.run(); line != "" || err != nil {
		t.Errorf("no installed unit, nothing to do, got %q, %v", line, err)
	}
}

func TestServiceRestarter_TimesOut(t *testing.T) {
	r, _ := fakeRestarter([]string{"100"}, "x")
	r.wait = 20 * time.Millisecond
	if _, err := r.run(); err == nil {
		t.Fatal("a service that keeps its old process must be reported")
	}
	r2, _ := fakeRestarter([]string{"100", "200"}, "x")
	r2.restart = func() error { return errors.New("unit failed") }
	if _, err := r2.run(); err == nil {
		t.Fatal("a failed restart must be reported")
	}
}

func TestServiceRestarter_StableDuringSettle(t *testing.T) {
	r, _ := fakeRestarter([]string{"100", "200"}, "/usr/local/bin/senhub-agent")
	r.settle = 30 * time.Millisecond
	if _, err := r.run(); err != nil {
		t.Fatalf("a service that stays up with the same PID must pass the settle window: %v", err)
	}
}

func TestServiceRestarter_PIDChangesDuringSettle(t *testing.T) {
	r, _ := fakeRestarter([]string{"100", "200", "300"}, "/usr/local/bin/senhub-agent")
	r.settle = time.Second
	_, err := r.run()
	if err == nil || !strings.Contains(err.Error(), "stopped within") {
		t.Fatalf("a PID change during settle must fail with 'stopped within', got %v", err)
	}
}

// The restore after a failed start must start a service the crash left
// stopped; an ordinary restart still leaves a stopped service alone.
func TestServiceRestarter_StartsAStoppedServiceWhenAsked(t *testing.T) {
	r, restarts := fakeRestarter([]string{"0", "300"}, "/usr/local/bin/senhub-agent")
	started := false
	r.isActive = func() bool { return started }
	r.restart = func() error { *restarts++; started = true; return nil }
	r.startIfStopped = true
	line, err := r.run()
	if err != nil || *restarts != 1 || !strings.Contains(line, "0.6.1") {
		t.Fatalf("a stopped service must be started when asked, got %q, %v, %d restarts", line, err, *restarts)
	}
}
