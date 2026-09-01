package configuration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
)

// TestStartRunsWithoutAConfigurationWatch is the acceptance condition of
// #850.
//
// The watch is a convenience: an edit picked up without a restart. It can
// fail for reasons that have nothing to do with the operator's file —
// inotify has a per-user instance quota, and a host running k3s can hold
// most of it. Treating it as a fatal start meant the agent exited,
// systemd restarted it five times, gave up, and the host stopped being
// monitored because a convenience could not start.
func TestStartRunsWithoutAConfigurationWatch(t *testing.T) {
	agentstate.ClearConfigWatchDisabled()
	t.Cleanup(agentstate.ClearConfigWatchDisabled)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: configPath}, createTestLocalLogger())
	lc.newWatcher = func() (*fsnotify.Watcher, error) {
		return nil, errors.New("too many open files")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := lc.Start(ctx); err != nil {
		t.Fatalf("the agent must run without a watch, got: %v", err)
	}
	t.Cleanup(func() { _ = lc.Shutdown(context.Background()) })

	// It runs, and it runs on the configuration: collection and export
	// are what the agent is for, and they are unaffected.
	if lc.GetConfiguration().Agent.AuthenticationKey == "" {
		t.Error("the configuration was not loaded")
	}

	state := agentstate.GetConfigWatchDisabled()
	if state == nil {
		t.Fatal("running unwatched must be reported as state, not only as a log line")
	}
	if state.Reason != agentstate.ConfigWatchUnavailable {
		t.Errorf("reason = %q, want %q", state.Reason, agentstate.ConfigWatchUnavailable)
	}
	if state.Detail == "" {
		t.Error("the state must carry what the kernel said")
	}
}

// TestStartClearsTheDegradedStateWhenTheWatchWorks: an agent that gets
// its watcher back must stop reporting, without anything else to do.
func TestStartClearsTheDegradedStateWhenTheWatchWorks(t *testing.T) {
	agentstate.RecordConfigWatchDisabled(agentstate.ConfigWatchUnavailable, "from an earlier run")
	t.Cleanup(agentstate.ClearConfigWatchDisabled)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: configPath}, createTestLocalLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = lc.Shutdown(context.Background()) })

	if state := agentstate.GetConfigWatchDisabled(); state != nil {
		t.Errorf("a working watch must clear the state, still reporting %+v", state)
	}
}

// TestAMalformedConfigurationStillFailsTheStart is the other half: what
// must NOT change. A file the operator wrote wrong is a start failure,
// and systemd is right to report it.
func TestAMalformedConfigurationStillFailsTheStart(t *testing.T) {
	agentstate.ClearConfigWatchDisabled()
	t.Cleanup(agentstate.ClearConfigWatchDisabled)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte("config_version: 3\nagent:\n  key: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lc := NewLocalConfiguration(&cliArgs.ParsedArgs{ConfigPath: configPath}, createTestLocalLogger())
	if err := lc.Start(context.Background()); err == nil {
		_ = lc.Shutdown(context.Background())
		t.Error("a malformed configuration must still fail the start")
	}
}
