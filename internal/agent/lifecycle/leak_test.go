package lifecycle_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/sensor"

	// The strategies register themselves with the data store; without
	// this the store starts zero strategies and the test would prove
	// nothing about strategy teardown.
	_ "senhub-agent.go/internal/agent/services/data_store/strategyreg"

	// Same for the probe constructors: the registry is populated by the
	// probe packages' init(), not by importing the probes package. The
	// cpu probe is the free-tier one every install runs.
	_ "senhub-agent.go/internal/agent/probes/cpu"
)

// TestNoGoroutineLeakAcrossStartReloadStop is the acceptance test of
// #285: a full start → reload → stop cycle, repeated, must not grow the
// goroutine count. Each cycle brings up the real configuration loader,
// data store (HTTP strategy on an ephemeral port) and probe pool, pushes
// a config change through the watcher, then stops everything.
//
// A leak here is not academic: the agent reloads its configuration on
// every operator edit, and a per-reload leak is what made long-running
// agents accumulate scheduler goroutines until the process was
// restarted.
func TestNoGoroutineLeakAcrossStartReloadStop(t *testing.T) {
	// One logger for the whole test, as the daemon has one for the whole
	// process. Building one per cycle would measure the log rotator's
	// per-logger mill goroutine (lumberjack keeps it for the logger's
	// lifetime and the logger has no Close, #835) instead of the service
	// lifecycle this test is about.
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})

	// One warm-up cycle absorbs the process-wide one-time work a first
	// bring-up does (lookup registry, transformer definitions, secret
	// backend) so the baseline measures a steady state, not a cold one.
	runOneCycle(t, baseLogger, -1)
	baseline := stableGoroutineCount(t)

	const cycles = 6
	for cycle := 0; cycle < cycles; cycle++ {
		runOneCycle(t, baseLogger, cycle)
	}

	after := stableGoroutineCount(t)
	t.Logf("goroutines: baseline=%d after %d cycles=%d", baseline, cycles, after)

	// A small tolerance absorbs runtime-owned goroutines that are not
	// ours (http transport idle-conn reapers, the fsnotify kqueue/inotify
	// poller finishing its teardown). A real leak is per-cycle and grows
	// with the loop count, so it blows past this immediately.
	const tolerance = 2
	if after > baseline+tolerance {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutine leak across start/reload/stop cycles: baseline=%d after=%d (tolerance %d)\n%s",
			baseline, after, tolerance, buf[:n])
	}
}

// runOneCycle starts the real service set on a throwaway config
// directory, mutates the config to force a reload, and stops everything
// through the supervisor.
func runOneCycle(t *testing.T, baseLogger *logger.Logger, cycle int) {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	port := freePort(t)
	writeConfig(t, configPath, port, 0)

	args := &cliArgs.ParsedArgs{ConfigPath: configPath}

	agentstate.ResetStrategyFailuresForTest()

	localConfig := configuration.NewLocalConfiguration(args, baseLogger)
	agentConfig := configuration.NewAgentConfigurationWithLocal("leak-test-key", localConfig, baseLogger)
	store := data_store.NewDataStore(agentConfig, localConfig, baseLogger)
	sensors := sensor.NewSensor(store.GetCallback(), localConfig, baseLogger)

	sup := lifecycle.NewSupervisor(baseLogger)
	if errs := sup.Start(context.Background(), localConfig, store, sensors); len(errs) > 0 {
		t.Fatalf("cycle %d: start errors: %v", cycle, errs)
	}

	// Guard against the test degrading into a hollow pass: if the
	// strategy or the probe never came up, the cycle exercises nothing
	// and a leak in their teardown would go unnoticed.
	if failures := agentstate.GetStrategyFailures(); len(failures) > 0 {
		t.Fatalf("cycle %d: strategy did not start, so the cycle proves nothing: %v", cycle, failures)
	}
	if total, _ := agentstate.GetProbeCounts(); total != 1 {
		t.Fatalf("cycle %d: want 1 active probe, got %d — the cycle proves nothing", cycle, total)
	}

	// Force a reload: a new probe interval means the sensor stops the
	// old poller and starts a new one, and the store rebuilds the HTTP
	// strategy. That is the path that used to leak.
	writeConfig(t, configPath, port, 1)
	time.Sleep(600 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(),
		lifecycle.TotalStopBudget(localConfig, store, sensors))
	defer cancel()

	if errs := sup.Shutdown(ctx); len(errs) > 0 {
		t.Fatalf("cycle %d: shutdown errors: %v", cycle, errs)
	}
}

// writeConfig writes a minimal single-file config. variant changes the
// probe interval so a rewrite is a real configuration change, not a
// no-op the loader would swallow.
func writeConfig(t *testing.T, path string, port, variant int) {
	t.Helper()

	interval := 30 + variant*10
	body := fmt.Sprintf(`config_version: 2
agent:
  key: "leak-test-key"

storage:
  - name: http
    params:
      port: %d
      bind_address: "127.0.0.1"
      endpoints: ["prtg"]

probes:
  - name: cpu-leak-test
    type: cpu
    interval: %ds
    params: {}
`, port, interval)

	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// freePort reserves an ephemeral port and releases it, so the HTTP
// strategy binds somewhere free. The strategy rejects port 0, and a
// rejected strategy is exactly the hollow pass this test must not be.
func freePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

// stableGoroutineCount waits for the goroutine count to settle, so the
// baseline is not taken while a previous test's teardown is still
// unwinding.
func stableGoroutineCount(t *testing.T) int {
	t.Helper()

	last := runtime.NumGoroutine()
	stable := 0
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
		n := runtime.NumGoroutine()
		if n == last {
			stable++
			if stable >= 3 {
				return n
			}
		} else {
			stable = 0
			last = n
		}
	}
	return runtime.NumGoroutine()
}
