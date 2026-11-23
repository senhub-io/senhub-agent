//go:build linux

package auto_update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stepOf(t *testing.T, err error) string {
	t.Helper()
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	return rerr.Step
}

func (f *rootApplyFixture) writeFile(t *testing.T, name, content string) {
	t.Helper()
	p := filepath.Join(f.reqDir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
}

func (f *rootApplyFixture) assertUntouched(t *testing.T) {
	t.Helper()
	if string(f.readBinary(t)) != "old binary" {
		t.Error("binary changed")
	}
	if _, err := os.Stat(f.opts.Binary + ".previous"); !os.IsNotExist(err) {
		t.Error(".previous exists")
	}
	if len(f.restartCalls) != 0 {
		t.Errorf("restart called %v", f.restartCalls)
	}
}

func TestRootApplyRefusesADowngrade(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.0\n")
	f.f.useOwnKey(t, list("0.7.2", "0.7.1", "0.7.0"), nil)
	err := RootApply(f.opts)
	if step := stepOf(t, err); step != "downgrade" {
		t.Fatalf("step = %q, want downgrade (%v)", step, err)
	}
	if len(f.f.downloads()) != 0 {
		t.Error("an archive was downloaded")
	}
	f.assertUntouched(t)
	f.assertRequestDeleted(t)
}

func TestRootApplyEditionPicksTheMatchingArchive(t *testing.T) {
	for _, tc := range []struct{ pkg, want, notWant string }{
		{"senhub-agent-oss", "/senhub-agent-oss-linux-", ""},
		{"senhub-agent", "/senhub-agent-linux-", "-oss-"},
	} {
		t.Run(tc.pkg, func(t *testing.T) {
			f := newRootApplyFixture(t, "0.7.2\n")
			f.opts.PackageName = func(string) string { return tc.pkg }
			if err := RootApply(f.opts); err != nil {
				t.Fatal(err)
			}
			d := f.f.downloads()
			if len(d) != 1 || !strings.Contains(d[0], tc.want) || (tc.notWant != "" && strings.Contains(d[0], tc.notWant)) {
				t.Fatalf("downloaded %v, want an archive containing %q", d, tc.want)
			}
		})
	}
}

func TestRootApplyRefusesAnUnknownEdition(t *testing.T) {
	for _, pkg := range []string{"", "senhub-agent-enterprise", "somebody-else"} {
		f := newRootApplyFixture(t, "0.7.2\n")
		f.opts.PackageName = func(string) string { return pkg }
		err := RootApply(f.opts)
		if step := stepOf(t, err); step != "edition" {
			t.Fatalf("%q: step = %q (%v)", pkg, step, err)
		}
		if f.f.hits.Load() != 0 {
			t.Errorf("%q: the registry was contacted", pkg)
		}
		f.assertUntouched(t)
		f.assertRequestDeleted(t)
	}
}

func TestPackageName(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "senhub-agent")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(exe)
	for list, want := range map[string]string{"senhub-agent-oss.list": "senhub-agent-oss", "senhub-agent-oss:arm64.list": "senhub-agent-oss", "senhub-agent.list": "senhub-agent"} {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, list), []byte("/usr/share/doc\n"+resolved+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := packageName(exe, d, nil); got != want {
			t.Errorf("%s: %q, want %q", list, got, want)
		}
	}
	if got := packageName(exe, dir, func(string) string { return "senhub-agent-oss" }); got != "senhub-agent-oss" {
		t.Errorf("rpm: %q", got)
	}
	if got := packageName(exe, dir, nil); got != "" {
		t.Errorf("no package: %q", got)
	}
}

func writePendingMarker(t *testing.T, f *rootApplyFixture, to string, attempts int) {
	t.Helper()
	data, _ := json.Marshal(pendingUpdate{FromVersion: "0.7.1", ToVersion: to, SwappedAt: time.Now(), BootAttempts: attempts})
	if err := os.WriteFile(filepath.Join(f.stateDir, pendingFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// installThenAskRollback installs 0.7.2 through the unit, then runs as 0.7.2.
func installThenAskRollback(t *testing.T, content string) *rootApplyFixture {
	t.Helper()
	f := newRootApplyFixture(t, "0.7.2\n")
	if err := RootApply(f.opts); err != nil {
		t.Fatal(err)
	}
	withRunningVersion(t, "0.7.2")
	f.restartCalls = nil
	writePendingMarker(t, f, "0.7.2", maxBootAttempts)
	f.writeFile(t, RootUpdateRollbackFile, content)
	return f
}

func TestRootRollbackRestoresThePreviousBinaryOnce(t *testing.T) {
	f := installThenAskRollback(t, "rollback\n")
	if err := RootApply(f.opts); err != nil {
		t.Fatal(err)
	}
	if string(f.readBinary(t)) != "old binary" {
		t.Error("the previous binary was not put back")
	}
	if len(f.restartCalls) != 1 {
		t.Errorf("restart calls = %v", f.restartCalls)
	}
	if n := len(f.f.downloads()); n != 1 {
		t.Errorf("downloads = %d, the rollback must download nothing", n)
	}
	f.writeFile(t, RootUpdateRollbackFile, "rollback\n")
	if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
		t.Errorf("a second rollback: step %q, want rollback", step)
	}
}

func TestRootRollbackRefusals(t *testing.T) {
	t.Run("nothing installed by the unit", func(t *testing.T) {
		f := newRootApplyFixture(t, "0.7.2\n")
		_ = os.Remove(filepath.Join(f.reqDir, RootUpdateRequestFile))
		f.writeFile(t, RootUpdateRollbackFile, "rollback\n")
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
		f.assertUntouched(t)
	})
	t.Run("running version is not the one installed", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		withRunningVersion(t, "0.7.5")
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
		if string(f.readBinary(t)) == "old binary" {
			t.Error("rolled back")
		}
	})
	t.Run("install too old", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		old, _ := json.Marshal(installRecord{From: "0.7.1", To: "0.7.2", At: time.Now().Add(-time.Hour)})
		if err := os.WriteFile(filepath.Join(f.rootDir, installRecordFile), old, 0o600); err != nil {
			t.Fatal(err)
		}
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
		if string(f.readBinary(t)) == "old binary" {
			t.Error("rolled back")
		}
	})
	t.Run("boot guard no longer pending (update confirmed)", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		_ = os.Remove(filepath.Join(f.stateDir, pendingFileName))
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
		if string(f.readBinary(t)) == "old binary" {
			t.Error("rolled back")
		}
	})
	t.Run("boot guard has not used its boots", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		writePendingMarker(t, f, "0.7.2", 1)
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
	})
	t.Run("boot guard marker of another version", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		writePendingMarker(t, f, "0.7.9", maxBootAttempts)
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
	})
	t.Run("marker is a symlink", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		p := filepath.Join(f.stateDir, pendingFileName)
		_ = os.Remove(p)
		if err := os.Symlink("/etc/hostname", p); err != nil {
			t.Fatal(err)
		}
		if step := stepOf(t, RootApply(f.opts)); step != "rollback" {
			t.Fatalf("step %q", step)
		}
	})
	for name, content := range map[string]string{"no newline": "rollback", "a version": "0.7.1\n", "extra": "rollback\nrollback\n", "empty": ""} {
		t.Run("malformed "+name, func(t *testing.T) {
			f := installThenAskRollback(t, content)
			if step := stepOf(t, RootApply(f.opts)); step != "request" {
				t.Fatalf("step %q", step)
			}
			if string(f.readBinary(t)) == "old binary" {
				t.Error("rolled back")
			}
		})
	}
	t.Run("forged through the version request", func(t *testing.T) {
		f := installThenAskRollback(t, "rollback\n")
		_ = os.Remove(filepath.Join(f.reqDir, RootUpdateRollbackFile))
		f.writeFile(t, RootUpdateRequestFile, "rollback\n")
		if step := stepOf(t, RootApply(f.opts)); step != "request" {
			t.Fatalf("step %q", step)
		}
		if string(f.readBinary(t)) == "old binary" {
			t.Error("rolled back")
		}
	})
}

func TestRequestRollbackWritesAFileOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := &packageInstaller{requestDir: dir, requestFile: RootUpdateRequestFile}
	if err := p.RequestRollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, RootUpdateRequestFile)); !os.IsNotExist(err) {
		t.Error("a rollback wrote the version request file")
	}
	uid := uint32(os.Getuid())
	if _, err := consumeRequest(dir, RootUpdateRollbackFile, uid, createTestModuleLogger()); err == nil {
		t.Error("a rollback request was read as a version")
	}
	if err := p.RequestRollback(); err != nil {
		t.Fatal(err)
	}
	if err := consumeRollbackRequest(dir, RootUpdateRollbackFile, uid, createTestModuleLogger()); err != nil {
		t.Errorf("rollback request refused: %v", err)
	}
}

func TestFleetUpdate_DowngradeOnAPackageInstall(t *testing.T) {
	for _, tc := range []struct {
		allow  bool
		reason string
	}{{false, CodeDowngradeNotAllowed}, {true, ReasonNotReplace}} {
		cleanUpdateState(t)
		withRunningVersion(t, "0.7.2")
		withInstallKind(t, KindDeb)
		f := newRegistryFake(t)
		f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
		u := f.updater(t, "linux", true)
		p := &fakePrivileged{available: true}
		RegisterPrivilegedInstaller(p)
		if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.1", AllowDowngrade: tc.allow}, f.srv.URL); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, StateFailed, "0.7.1", tc.reason)
		if len(p.asked) != 0 {
			t.Errorf("allow=%v: the root unit was asked %v", tc.allow, p.asked)
		}
	}
}
