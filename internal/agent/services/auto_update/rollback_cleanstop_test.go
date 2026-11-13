package auto_update

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanStop_DoesNotCountTheBoot(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })

	putJSON(t, fs, filepath.Join("/state", pendingFileName), pendingUpdate{
		FromVersion:  "0.7.1",
		ToVersion:    "0.7.2",
		SwappedAt:    now,
		BootAttempts: 0,
	})

	for i := 0; i < 3; i++ {
		action := env.boot()
		if action == bootRolledBack || action == bootRollbackNotPerformed {
			t.Fatalf("boot %d rolled back unexpectedly: %v", i+1, action)
		}
		if err := env.cleanStop(); err != nil {
			t.Fatalf("cleanStop: %v", err)
		}
	}

	data, err := fs.ReadFile(filepath.Join("/state", pendingFileName))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if marker.BootAttempts != 0 {
		t.Fatalf("BootAttempts = %d after three clean stops, want 0", marker.BootAttempts)
	}
}

func TestCrashesStillRollBack(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })

	fs.files["/opt/senhub-agent/senhub-agent"] = []byte("new")
	fs.files["/opt/senhub-agent/senhub-agent.previous"] = []byte("old")

	putJSON(t, fs, filepath.Join("/state", pendingFileName), pendingUpdate{
		FromVersion:  "0.7.1",
		ToVersion:    "0.7.2",
		SwappedAt:    now,
		BootAttempts: 0,
	})

	for i := 0; i < 2; i++ {
		if action := env.boot(); action == bootRolledBack || action == bootRollbackNotPerformed {
			t.Fatalf("boot %d rolled back too early: %v", i+1, action)
		}
	}
	if action := env.boot(); action != bootRolledBack {
		t.Fatalf("3rd boot action = %v, want bootRolledBack", action)
	}
	if _, ok := fs.files["/opt/senhub-agent/senhub-agent.previous"]; ok {
		t.Fatal("previous binary still present after rollback")
	}
}

func TestCleanStop_IgnoresOtherVersionAndMissingMarker(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })

	putJSON(t, fs, filepath.Join("/state", pendingFileName), pendingUpdate{
		FromVersion:  "0.7.1",
		ToVersion:    "0.7.3",
		SwappedAt:    now,
		BootAttempts: 2,
	})
	if err := env.cleanStop(); err != nil {
		t.Fatalf("cleanStop: %v", err)
	}
	data, _ := fs.ReadFile(filepath.Join("/state", pendingFileName))
	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if marker.BootAttempts != 2 {
		t.Fatalf("BootAttempts changed to %d, want 2", marker.BootAttempts)
	}

	fs2 := newFakeFS()
	env2 := newTestRollbackEnv(t, fs2, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })
	if err := env2.cleanStop(); err != nil {
		t.Fatalf("cleanStop without marker: %v", err)
	}
	if len(fs2.files) != 0 {
		t.Fatalf("files created without marker: %v", fs2.files)
	}
}

func TestStagedAwaitingRestart(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.1", true, func() time.Time { return now })

	putJSON(t, fs, filepath.Join("/state", pendingFileName), pendingUpdate{
		FromVersion:  "0.7.0",
		ToVersion:    "0.7.2",
		SwappedAt:    now,
		BootAttempts: 0,
	})

	if !env.stagedAwaitingRestart("0.7.2") {
		t.Error("stagedAwaitingRestart(0.7.2) = false, want true")
	}
	if !env.stagedAwaitingRestart("v0.7.2") {
		t.Error("stagedAwaitingRestart(v0.7.2) = false, want true")
	}
	if env.stagedAwaitingRestart("0.7.3") {
		t.Error("stagedAwaitingRestart(0.7.3) = true, want false")
	}

	envSame := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })
	if envSame.stagedAwaitingRestart("0.7.2") {
		t.Error("stagedAwaitingRestart with same version = true, want false")
	}

	fsEmpty := newFakeFS()
	envEmpty := newTestRollbackEnv(t, fsEmpty, "/opt/senhub-agent/senhub-agent", "/state", "0.7.1", true, func() time.Time { return now })
	if envEmpty.stagedAwaitingRestart("0.7.2") {
		t.Error("stagedAwaitingRestart without marker = true, want false")
	}
}

func TestBootGuard_StoppedCleanlyAfterConfirmDoesNothing(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })

	putJSON(t, fs, filepath.Join("/state", pendingFileName), pendingUpdate{
		FromVersion:  "0.7.1",
		ToVersion:    "0.7.2",
		SwappedAt:    now,
		BootAttempts: 1,
	})

	g := &BootGuard{env: env, action: bootTracking, settled: true}
	g.StoppedCleanly()

	data, _ := fs.ReadFile(filepath.Join("/state", pendingFileName))
	var marker pendingUpdate
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if marker.BootAttempts != 1 {
		t.Fatalf("BootAttempts = %d, want 1", marker.BootAttempts)
	}

	var nilGuard *BootGuard
	nilGuard.StoppedCleanly()
}

// The real guard, not a fake: a clean stop takes this boot back once, and
// a second stop or a later confirmation does nothing more.
func TestBootGuard_CleanStopTakesTheBootBackOnce(t *testing.T) {
	fs := newFakeFS()
	now := time.Now()
	env := newTestRollbackEnv(t, fs, "/opt/senhub-agent/senhub-agent", "/state", "0.7.2", true, func() time.Time { return now })
	path := filepath.Join("/state", pendingFileName)
	putJSON(t, fs, path, pendingUpdate{FromVersion: "0.7.1", ToVersion: "0.7.2", SwappedAt: now, BootAttempts: 2})

	g := &BootGuard{env: env, action: bootTracking}
	g.StoppedCleanly()
	g.StoppedCleanly()

	var marker pendingUpdate
	if err := json.Unmarshal(fs.files[path], &marker); err != nil {
		t.Fatal(err)
	}
	if marker.BootAttempts != 1 {
		t.Fatalf("BootAttempts = %d, want 1 (one clean stop counted once)", marker.BootAttempts)
	}
	if !g.settled {
		t.Fatal("a clean stop must settle the guard so the delayed confirmation does not run after it")
	}
}
