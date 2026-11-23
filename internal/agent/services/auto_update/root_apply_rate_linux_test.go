//go:build linux

package auto_update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func (f *rootApplyFixture) askDowngrade(t *testing.T) error {
	t.Helper()
	f.writeFile(t, RootUpdateRequestFile, "0.7.0\n")
	return RootApply(f.opts)
}

func TestRootApplyRateLimitsToSixAttemptsAnHourAndRecovers(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.0\n")
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	f.opts.Now = func() time.Time { return now }
	for i := 1; i <= 6; i++ {
		now = now.Add(time.Minute)
		if step := stepOf(t, f.askDowngrade(t)); step != "downgrade" {
			t.Fatalf("attempt %d: step %q, want downgrade", i, step)
		}
	}
	now = now.Add(time.Minute)
	if step := stepOf(t, f.askDowngrade(t)); step != "rate" {
		t.Fatalf("the 7th attempt: step %q, want rate", step)
	}
	if r, _ := ReadUpdateResult(f.stateDir); r == nil || r.Outcome != ResultRefused || r.Reason != ReasonRateLimited {
		t.Errorf("result = %+v", r)
	}
	f.assertRequestDeleted(t)

	// A refused attempt is not counted: refusing again does not push the
	// window out. One hour after the first attempt, the first slot is free.
	now = time.Date(2026, 11, 23, 11, 1, 30, 0, time.UTC)
	if step := stepOf(t, f.askDowngrade(t)); step != "downgrade" {
		t.Fatalf("after the window: step %q, want downgrade", step)
	}
	now = now.Add(5 * time.Second)
	if step := stepOf(t, f.askDowngrade(t)); step != "rate" {
		t.Fatalf("only one slot freed: step %q, want rate", step)
	}
}

func TestRootRollbackIsNeverRateLimited(t *testing.T) {
	f := installThenAskRollback(t, "rollback\n")
	now := time.Now()
	var recent []int64
	for i := 0; i < 6; i++ {
		recent = append(recent, now.Add(-time.Duration(i)*time.Minute).Unix())
	}
	data, _ := json.Marshal(recent)
	if err := os.WriteFile(filepath.Join(f.rootDir, attemptsFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RootApply(f.opts); err != nil {
		t.Fatalf("a rollback with a full attempts record: %v", err)
	}
	if string(f.readBinary(t)) != "old binary" {
		t.Error("the previous binary was not put back")
	}
	if r, _ := ReadUpdateResult(f.stateDir); r == nil || r.Outcome != ResultRolledBack {
		t.Errorf("result = %+v", r)
	}
	// The same full record refuses a version request.
	f.writeFile(t, RootUpdateRequestFile, "0.7.5\n")
	if step := stepOf(t, RootApply(f.opts)); step != "rate" {
		t.Errorf("version request: step %q, want rate", step)
	}
}

// Every way a run can end badly leaves no request behind, because the path
// unit would start the service again for as long as one exists, with no
// systemd start limit to stop it.
func TestRootApplyEveryRefusalConsumesTheRequest(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, f *rootApplyFixture)
		step   string
		reason string
	}{
		{"service user unknown", func(t *testing.T, f *rootApplyFixture) {
			f.opts.ServiceUID = func() (uint32, error) { return 0, errors.New("no such user") }
		}, "request", "request"},
		{"malformed content", func(t *testing.T, f *rootApplyFixture) { f.writeFile(t, RootUpdateRequestFile, "latest\n") }, "request", "request"},
		{"too long", func(t *testing.T, f *rootApplyFixture) {
			f.writeFile(t, RootUpdateRequestFile, "0.7.2\n"+string(make([]byte, 100)))
		}, "request", "request"},
		{"wrong owner", func(t *testing.T, f *rootApplyFixture) {
			f.opts.ServiceUID = func() (uint32, error) { return uint32(os.Getuid()) + 1, nil }
		}, "request", "request"},
		{"rate limited", func(t *testing.T, f *rootApplyFixture) {
			f.opts.RateBurst = 1
			f.opts.RateWindow = time.Hour
			fillAttempts(t, f, 1)
		}, "rate", ReasonRateLimited},
		{"edition unknown", func(t *testing.T, f *rootApplyFixture) { f.opts.PackageName = func(string) string { return "" } }, "edition", ReasonNotReplace},
		{"downgrade", func(t *testing.T, f *rootApplyFixture) { f.writeFile(t, RootUpdateRequestFile, "0.7.0\n") }, "downgrade", CodeDowngradeNotAllowed},
		{"list signature", func(t *testing.T, f *rootApplyFixture) { f.f.stableSig = []byte("not a signature") }, "list", ReasonSignature},
		{"not published", func(t *testing.T, f *rootApplyFixture) { f.writeFile(t, RootUpdateRequestFile, "0.9.9\n") }, "published", CodeTargetNotPublished},
		{"archive signature", func(t *testing.T, f *rootApplyFixture) { f.f.archiveSig = []byte("not a signature") }, "install", ReasonSignature},
		{"lock held", func(t *testing.T, f *rootApplyFixture) {
			release, err := acquireRootLock(f.rootDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
		}, "lock", "busy"},
		{"rollback with nothing to go back to", func(t *testing.T, f *rootApplyFixture) {
			_ = os.Remove(filepath.Join(f.reqDir, RootUpdateRequestFile))
			f.writeFile(t, RootUpdateRollbackFile, "rollback\n")
		}, "rollback", "rollback"},
		{"malformed rollback", func(t *testing.T, f *rootApplyFixture) { f.writeFile(t, RootUpdateRollbackFile, "rollback") }, "request", "request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRootApplyFixture(t, "0.7.2\n")
			tc.setup(t, f)
			if step := stepOf(t, RootApply(f.opts)); step != tc.step {
				t.Fatalf("step %q, want %q", step, tc.step)
			}
			f.assertRequestDeleted(t)
			if _, err := os.Lstat(filepath.Join(f.reqDir, RootUpdateRollbackFile)); !os.IsNotExist(err) {
				t.Error("the rollback request is still there")
			}
			if tc.name != "lock held" {
				if r, _ := ReadUpdateResult(f.stateDir); r == nil || r.Outcome != ResultRefused || r.Reason != tc.reason {
					t.Errorf("result = %+v, want refused/%s", r, tc.reason)
				}
			}
			if string(f.readBinary(t)) != "old binary" {
				t.Error("something was installed")
			}
		})
	}

	t.Run("panic", func(t *testing.T) {
		f := newRootApplyFixture(t, "0.7.2\n")
		f.opts.PackageName = func(string) string { panic("boom") }
		func() {
			defer func() { _ = recover() }()
			_ = RootApply(f.opts)
		}()
		f.assertRequestDeleted(t)
	})
}

func fillAttempts(t *testing.T, f *rootApplyFixture, n int) {
	t.Helper()
	var times []int64
	for i := 0; i < n; i++ {
		times = append(times, time.Now().Add(-time.Minute).Unix())
	}
	data, _ := json.Marshal(times)
	if err := os.WriteFile(filepath.Join(f.rootDir, attemptsFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWriteResultFile(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("KEEP"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, updateResultFile)); err != nil {
		t.Fatal(err)
	}
	res := UpdateResult{Version: "0.7.2", Outcome: ResultRefused, Reason: ReasonRateLimited, At: time.Now().UTC()}
	if err := writeResultFile(dir, res); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "KEEP" {
		t.Fatal("the planted link was followed")
	}
	info, err := os.Lstat(filepath.Join(dir, updateResultFile))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("result file: %v, %v", info, err)
	}
	got, err := ReadUpdateResult(dir)
	if err != nil || got == nil || got.Reason != ReasonRateLimited || got.Version != "0.7.2" {
		t.Fatalf("read back: %+v, %v", got, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".update-result-*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
	link := filepath.Join(t.TempDir(), "dirlink")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := writeResultFile(link, res); err == nil {
		t.Error("a directory reached through a link was written")
	}
}

func TestServiceReadsTheRootResultIntoTheStatus(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindDeb)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
	u := f.updater(t, "linux", true)
	fast := 5 * time.Millisecond
	u.resultPoll = &fast
	RegisterPrivilegedInstaller(&fakePrivileged{available: true})

	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateInstalling, "0.7.2", "")

	// A result about another version, or older than the request, changes nothing.
	if err := os.MkdirAll(u.stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(r UpdateResult) {
		data, _ := json.Marshal(r)
		if err := os.WriteFile(filepath.Join(u.stateDir, updateResultFile), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(UpdateResult{Version: "0.7.9", Outcome: ResultRefused, Reason: ReasonRateLimited, At: time.Now()})
	time.Sleep(60 * time.Millisecond)
	wantStatus(t, StateInstalling, "0.7.2", "")

	write(UpdateResult{Version: "", Outcome: ResultRefused, Reason: ReasonRateLimited, At: time.Now()})
	deadline := time.Now().Add(3 * time.Second)
	for CurrentStatus().State != StateFailed && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	wantStatus(t, StateFailed, "0.7.2", ReasonRateLimited)
}
