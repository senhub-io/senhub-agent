package auto_update

import (
	"path/filepath"
	"testing"
	"time"
)

func bootThirdAttempt(t *testing.T, fs *fakeFS, stateDir string) bootAction {
	t.Helper()
	now := time.Now()
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "0.7.1", ToVersion: "0.7.2", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})
	env := newTestRollbackEnv(t, fs, filepath.Join(t.TempDir(), "senhub-agent"), stateDir, "0.7.2", false, func() time.Time { return now })
	return env.boot()
}

func TestBoot_FailedUpdateAsksTheRootUnitToGoBack(t *testing.T) {
	cleanUpdateState(t)
	p := &fakePrivileged{available: true}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()

	if got := bootThirdAttempt(t, fs, "/state"); got != bootRollbackNotPerformed {
		t.Fatalf("boot = %v", got)
	}
	if len(p.asked) != 1 || p.asked[0] != "0.7.1" {
		t.Fatalf("root unit asked for %v, want [0.7.1]", p.asked)
	}
	if rec := readRolledBackRecord(t, fs, "/state"); rec.Outcome != OutcomeRequested || rec.FailedVersion != "0.7.2" || rec.RestoredVersion != "0.7.1" {
		t.Errorf("record = %+v", rec)
	}
}

func TestBoot_NoRootUnitKeepsTheManualRestoreMessage(t *testing.T) {
	cleanUpdateState(t)
	p := &fakePrivileged{available: false}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()

	bootThirdAttempt(t, fs, "/state")
	if len(p.asked) != 0 {
		t.Fatalf("an unavailable unit was asked: %v", p.asked)
	}
	if rec := readRolledBackRecord(t, fs, "/state"); rec.Outcome != OutcomeNotPerformed {
		t.Errorf("outcome = %q", rec.Outcome)
	}
}

func TestBoot_DoesNotReturnToAVersionThatAlreadyFailed(t *testing.T) {
	cleanUpdateState(t)
	p := &fakePrivileged{available: true}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()
	putJSON(t, fs, filepath.Join("/state", rolledBackFileName), RolledBackRecord{FailedVersion: "0.7.1", RestoredVersion: "0.7.0", Outcome: OutcomeRestored})

	bootThirdAttempt(t, fs, "/state")
	if len(p.asked) != 0 {
		t.Fatalf("two failing versions would swap places: asked %v", p.asked)
	}
}
