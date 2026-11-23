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

type rootApplyFixture struct {
	f                  *registryFake
	reqDir             string
	binDir             string
	stateDir           string
	rootDir            string
	opts               RootApplyOptions
	restartCalls       []bool
	restartFailFirst   bool
	restartFailErrMsg  string
	restartShouldFail  bool
	restartCalledAfter *bool
}

func newRestartFake(fixture *rootApplyFixture) func(bool) (string, error) {
	return func(startIfStopped bool) (string, error) {
		fixture.restartCalls = append(fixture.restartCalls, startIfStopped)
		if fixture.restartFailFirst && len(fixture.restartCalls) == 1 {
			return "", errors.New("restart service failure")
		}
		return "restarted", nil
	}
}

func newRootApplyFixture(t *testing.T, request string) *rootApplyFixture {
	t.Helper()
	cleanUpdateState(t)

	f := &rootApplyFixture{
		reqDir:   t.TempDir(),
		binDir:   t.TempDir(),
		stateDir: t.TempDir(),
		rootDir:  t.TempDir(),
	}

	// chmod reqDir to 0750
	if err := os.Chmod(f.reqDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// write request file
	reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
	if err := os.WriteFile(reqPath, []byte(request), 0o640); err != nil {
		t.Fatal(err)
	}

	// write binary
	binPath := filepath.Join(f.binDir, "senhub-agent")
	if err := os.WriteFile(binPath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// set up registry fake with default lists
	f.f = newRegistryFake(t)
	f.f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)

	// set running version
	withRunningVersion(t, "0.7.1")

	// create restart fake
	f.restartCalledAfter = new(bool)
	restart := newRestartFake(f)

	zero := time.Duration(0)
	f.opts = RootApplyOptions{
		Logger:        createTestModuleLogger(),
		RequestDir:    f.reqDir,
		RequestFile:   RootUpdateRequestFile,
		ServiceUID:    func() (uint32, error) { return uint32(os.Getuid()), nil },
		Registry:      f.f.srv.URL,
		HTTPClient:    f.f.srv.Client(),
		Binary:        binPath,
		StateDir:      f.stateDir,
		RootStateDir:  f.rootDir,
		Restart:       restart,
		PackageName:   func(string) string { return "senhub-agent" },
		DownloadWait:  &zero,
		ListRetryWait: &zero,
	}

	return f
}

func (f *rootApplyFixture) readBinary(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.opts.Binary)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *rootApplyFixture) assertNoInstall(t *testing.T) {
	t.Helper()
	// binary unchanged
	if got := f.readBinary(t); string(got) != "old binary" {
		t.Errorf("binary content = %q, want %q", got, "old binary")
	}
	// no .previous
	prev := f.opts.Binary + ".previous"
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Errorf(".previous file exists, want absent")
	}
	// no downloads
	if got := len(f.f.downloads()); got != 0 {
		t.Errorf("downloads = %d, want 0", got)
	}
	// restart never called
	if len(f.restartCalls) != 0 {
		t.Errorf("restart called %d times, want 0", len(f.restartCalls))
	}
}

func (f *rootApplyFixture) assertRequestDeleted(t *testing.T) {
	t.Helper()
	reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Errorf("request file still exists, want deleted")
	}
}

func (f *rootApplyFixture) writeRootSeq(t *testing.T, seq map[string]int64) {
	t.Helper()
	data, err := json.Marshal(seq)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.rootDir, "update-list-seq.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *rootApplyFixture) writeStateSeq(t *testing.T, seq map[string]int64) {
	t.Helper()
	data, err := json.Marshal(seq)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.stateDir, "update-list-seq.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRootApplyValidRequestInstalls(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")

	err := RootApply(f.opts)
	if err != nil {
		t.Fatalf("RootApply returned error: %v", err)
	}

	// binary replaced
	if got := f.readBinary(t); string(got) == "old binary" {
		t.Errorf("binary not replaced")
	}
	// .previous holds old binary
	prevData, err := os.ReadFile(f.opts.Binary + ".previous")
	if err != nil {
		t.Fatalf("cannot read .previous: %v", err)
	}
	if string(prevData) != "old binary" {
		t.Errorf(".previous = %q, want %q", prevData, "old binary")
	}
	// request deleted
	f.assertRequestDeleted(t)
	// update-pending.json exists in stateDir
	pending := filepath.Join(f.stateDir, "update-pending.json")
	if _, err := os.Stat(pending); err != nil {
		t.Errorf("update-pending.json missing: %v", err)
	}
	// update-list-seq.json exists in rootDir but not stateDir
	rootSeq := filepath.Join(f.rootDir, "update-list-seq.json")
	if _, err := os.Stat(rootSeq); err != nil {
		t.Errorf("root update-list-seq.json missing: %v", err)
	}
	stateSeq := filepath.Join(f.stateDir, "update-list-seq.json")
	if _, err := os.Stat(stateSeq); !os.IsNotExist(err) {
		t.Errorf("state update-list-seq.json exists, want absent")
	}
	// restart called once with false
	if len(f.restartCalls) != 1 || f.restartCalls[0] != false {
		t.Errorf("restart calls = %v, want [false]", f.restartCalls)
	}
}

func TestRootApplyValidBetaRequestInstalls(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.3-beta.1\n")
	// override lists to include beta
	f.f.useOwnKey(t, list("0.7.2"), list("0.7.3-beta.1"))

	err := RootApply(f.opts)
	if err != nil {
		t.Fatalf("RootApply returned error: %v", err)
	}
	if got := f.readBinary(t); string(got) == "old binary" {
		t.Errorf("binary not replaced")
	}
}

func TestRootApplyRefusesMalformedRequests(t *testing.T) {
	tests := []struct {
		name   string
		modify func(f *rootApplyFixture, t *testing.T)
	}{
		{
			name: "malformed content",
			modify: func(f *rootApplyFixture, t *testing.T) {
				reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
				if err := os.WriteFile(reqPath, []byte("not-a-version\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "too long",
			modify: func(f *rootApplyFixture, t *testing.T) {
				reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
				if err := os.WriteFile(reqPath, []byte("0.1.2.3.4.5.6.7.8.9\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			modify: func(f *rootApplyFixture, t *testing.T) {
				reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
				if err := os.Remove(reqPath); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "valid-version")
				if err := os.WriteFile(target, []byte("0.7.2\n"), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, reqPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong owner",
			modify: func(f *rootApplyFixture, t *testing.T) {
				f.opts.ServiceUID = func() (uint32, error) { return uint32(os.Getuid()) + 1, nil }
			},
		},
		{
			name: "group writable",
			modify: func(f *rootApplyFixture, t *testing.T) {
				reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
				if err := os.Chmod(reqPath, 0o664); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRootApplyFixture(t, "0.7.2\n")
			tt.modify(f, t)

			err := RootApply(f.opts)
			var rerr *RootApplyError
			if !errors.As(err, &rerr) {
				t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
			}
			if rerr.Step != "request" {
				t.Errorf("Step = %q, want %q", rerr.Step, "request")
			}
			f.assertNoInstall(t)
			f.assertRequestDeleted(t)
		})
	}
}

func TestRootApplyRefusesNotPublished(t *testing.T) {
	f := newRootApplyFixture(t, "0.9.9\n")

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "published" {
		t.Errorf("Step = %q, want %q", rerr.Step, "published")
	}
	f.assertNoInstall(t)
	f.assertRequestDeleted(t)
}

func TestRootApplyRefusesBadListSignature(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	// tamper with the stable signature: flip a byte in the base64 body
	sig := f.f.stableSig
	if len(sig) < 10 {
		t.Fatal("signature too short")
	}
	idx := len(sig) / 2
	if sig[idx] == 'A' {
		sig[idx] = 'B'
	} else {
		sig[idx] = 'A'
	}
	f.f.stableSig = sig

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "list" {
		t.Errorf("Step = %q, want %q", rerr.Step, "list")
	}
	f.assertNoInstall(t)
	f.assertRequestDeleted(t)
}

func TestRootApplyRefusesWrongChannelSignature(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	// use beta signature for stable list
	f.f.stableSig = f.f.betaSig

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "list" {
		t.Errorf("Step = %q, want %q", rerr.Step, "list")
	}
	f.assertNoInstall(t)
	f.assertRequestDeleted(t)
}

func TestRootApplyRefusesReplayedOldList(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	f.writeRootSeq(t, map[string]int64{"stable": 1800000000})

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "list" {
		t.Errorf("Step = %q, want %q", rerr.Step, "list")
	}
	f.assertNoInstall(t)
	f.assertRequestDeleted(t)
}

func TestRootApplySeqFloorLocation(t *testing.T) {
	t.Run("state low floor ignored, root high floor refuses", func(t *testing.T) {
		f := newRootApplyFixture(t, "0.7.2\n")
		f.writeStateSeq(t, map[string]int64{"stable": 1})
		f.writeRootSeq(t, map[string]int64{"stable": 1800000000})

		err := RootApply(f.opts)
		var rerr *RootApplyError
		if !errors.As(err, &rerr) {
			t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
		}
		if rerr.Step != "list" {
			t.Errorf("Step = %q, want %q", rerr.Step, "list")
		}
		f.assertNoInstall(t)
		f.assertRequestDeleted(t)
	})

	t.Run("high floor in state without root does not refuse", func(t *testing.T) {
		f := newRootApplyFixture(t, "0.7.2\n")
		f.writeStateSeq(t, map[string]int64{"stable": 2000000000})

		err := RootApply(f.opts)
		if err != nil {
			t.Fatalf("RootApply returned error: %v", err)
		}
		// installation should have succeeded
		if got := f.readBinary(t); string(got) == "old binary" {
			t.Errorf("binary not replaced")
		}
	})
}

func TestRootApplyRefusesBadArchiveSignature(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	// truncate archive signature to cause failure
	f.f.archiveSig = f.f.archiveSig[:10]

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "install" {
		t.Errorf("Step = %q, want %q", rerr.Step, "install")
	}
	// binary unchanged
	if got := f.readBinary(t); string(got) != "old binary" {
		t.Errorf("binary content = %q, want %q", got, "old binary")
	}
	// no .previous
	if _, err := os.Stat(f.opts.Binary + ".previous"); !os.IsNotExist(err) {
		t.Errorf(".previous file exists, want absent")
	}
	// downloads happened (archive was fetched but not installed)
	if got := len(f.f.downloads()); got == 0 {
		t.Errorf("downloads = 0, want > 0")
	}
	f.assertRequestDeleted(t)
}

func TestRootApplyRunningVersionNoOp(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.1\n")

	err := RootApply(f.opts)
	if err != nil {
		t.Fatalf("RootApply returned error: %v", err)
	}
	// binary unchanged
	if got := f.readBinary(t); string(got) != "old binary" {
		t.Errorf("binary content = %q, want %q", got, "old binary")
	}
	// no .previous
	if _, err := os.Stat(f.opts.Binary + ".previous"); !os.IsNotExist(err) {
		t.Errorf(".previous file exists, want absent")
	}
	// no downloads
	if got := len(f.f.downloads()); got != 0 {
		t.Errorf("downloads = %d, want 0", got)
	}
	// restart not called
	if len(f.restartCalls) != 0 {
		t.Errorf("restart called %d times, want 0", len(f.restartCalls))
	}
	f.assertRequestDeleted(t)
}

func TestRootApplyLockHeld(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	release, err := acquireRootLock(f.rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	err = RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "lock" {
		t.Errorf("Step = %q, want %q", rerr.Step, "lock")
	}
	f.assertNoInstall(t)
	// request may or may not be deleted? consumeRequest is called after lock, so request should still exist
	reqPath := filepath.Join(f.reqDir, RootUpdateRequestFile)
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Errorf("request file exists after lock failure, want deleted")
	}
}

func TestRootApplyRestartFailureRollsBack(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	f.restartFailFirst = true

	err := RootApply(f.opts)
	var rerr *RootApplyError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
	}
	if rerr.Step != "restart" {
		t.Errorf("Step = %q, want %q", rerr.Step, "restart")
	}
	// binary should be restored to old binary
	if got := f.readBinary(t); string(got) != "old binary" {
		t.Errorf("binary content = %q, want %q", got, "old binary")
	}
	// .previous should not exist (restored removed it?)
	prev := f.opts.Binary + ".previous"
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Errorf(".previous still exists, want absent")
	}
	// restart called twice: first false (failed), then true (rollback restart)
	if len(f.restartCalls) != 2 || f.restartCalls[0] != false || f.restartCalls[1] != true {
		t.Errorf("restart calls = %v, want [false true]", f.restartCalls)
	}
	f.assertRequestDeleted(t)
}

func TestRootApplyRefusesMaliciousContent(t *testing.T) {
	tests := []struct {
		req string
	}{
		{"--registry=http://evil\n"},
		{"../../etc/passwd\n"},
	}

	for _, tt := range tests {
		t.Run(tt.req, func(t *testing.T) {
			f := newRootApplyFixture(t, tt.req)

			err := RootApply(f.opts)
			var rerr *RootApplyError
			if !errors.As(err, &rerr) {
				t.Fatalf("expected *RootApplyError, got %T: %v", err, err)
			}
			if rerr.Step != "request" {
				t.Errorf("Step = %q, want %q", rerr.Step, "request")
			}
			f.assertNoInstall(t)
			f.assertRequestDeleted(t)
			// no network hits
			if got := f.f.hits.Load(); got != 0 {
				t.Errorf("network hits = %d, want 0", got)
			}
		})
	}
}
