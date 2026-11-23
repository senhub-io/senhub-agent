package auto_update

import (
	"path/filepath"
	"testing"
	"time"
)

type fakeRollbackRoot struct {
	available bool
	asked     int
	versions  []string
}

func (p *fakeRollbackRoot) Available() bool { return p.available }
func (p *fakeRollbackRoot) Request(v string) error {
	p.versions = append(p.versions, v)
	return nil
}
func (p *fakeRollbackRoot) RequestRollback() error { p.asked++; return nil }

func bootThirdAttempt(t *testing.T, fs *fakeFS, stateDir string) bootAction {
	t.Helper()
	now := time.Now()
	putPending(t, fs, stateDir, pendingUpdate{FromVersion: "0.7.1", ToVersion: "0.7.2", SwappedAt: now.Add(-time.Minute), BootAttempts: 2})
	env := newTestRollbackEnv(t, fs, filepath.Join(t.TempDir(), "senhub-agent"), stateDir, "0.7.2", false, func() time.Time { return now })
	return env.boot()
}

func TestBoot_FailedUpdateAsksTheRootUnitToGoBack(t *testing.T) {
	cleanUpdateState(t)
	p := &fakeRollbackRoot{available: true}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()

	if got := bootThirdAttempt(t, fs, "/state"); got != bootRollbackNotPerformed {
		t.Fatalf("boot = %v", got)
	}
	if p.asked != 1 || len(p.versions) != 0 {
		t.Fatalf("root unit asked for %d rollback(s) and versions %v; a rollback must carry no version", p.asked, p.versions)
	}
	if _, err := fs.ReadFile(filepath.Join("/state", pendingFileName)); err != nil {
		t.Error("the boot guard marker must stay: the root unit reads it as proof")
	}
	if rec := readRolledBackRecord(t, fs, "/state"); rec.Outcome != OutcomeRequested || rec.FailedVersion != "0.7.2" || rec.RestoredVersion != "0.7.1" {
		t.Errorf("record = %+v", rec)
	}
}

func TestBoot_NoRootUnitKeepsTheManualRestoreMessage(t *testing.T) {
	cleanUpdateState(t)
	p := &fakeRollbackRoot{available: false}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()

	bootThirdAttempt(t, fs, "/state")
	if p.asked != 0 {
		t.Fatalf("an unavailable unit was asked")
	}
	if rec := readRolledBackRecord(t, fs, "/state"); rec.Outcome != OutcomeNotPerformed {
		t.Errorf("outcome = %q", rec.Outcome)
	}
}

func TestBoot_DoesNotReturnToAVersionThatAlreadyFailed(t *testing.T) {
	cleanUpdateState(t)
	p := &fakeRollbackRoot{available: true}
	RegisterPrivilegedInstaller(p)
	fs := newFakeFS()
	putJSON(t, fs, filepath.Join("/state", rolledBackFileName), RolledBackRecord{FailedVersion: "0.7.1", RestoredVersion: "0.7.0", Outcome: OutcomeRestored})

	bootThirdAttempt(t, fs, "/state")
	if p.asked != 0 {
		t.Fatalf("two failing versions would swap places")
	}
}

func TestBoot_AnInstallerWithoutRollbackIsNotAskedForAVersion(t *testing.T) {
	cleanUpdateState(t)
	p := &fakePrivileged{available: true}
	RegisterPrivilegedInstaller(p)
	bootThirdAttempt(t, newFakeFS(), "/state")
	if len(p.asked) != 0 {
		t.Fatalf("a version request stands for no rollback: asked %v", p.asked)
	}
}
