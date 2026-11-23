package auto_update

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aead.dev/minisign"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
)

const vectorsDir = "testdata/update-vectors"

func vector(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectorsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withBenchKey injects the public key the bench vectors were signed with, the
// way the other tests inject the release key.
func withBenchKey(t *testing.T) {
	t.Helper()
	pub := strings.Split(strings.TrimSpace(string(vector(t, "bench-minisign.pub"))), "\n")
	old := signingPublicKey
	signingPublicKey = pub[len(pub)-1]
	t.Cleanup(func() { signingPublicKey = old })
}

func withRunningVersion(t *testing.T, v string) {
	t.Helper()
	old := cliArgs.Version
	cliArgs.Version = v
	t.Cleanup(func() { cliArgs.Version = old })
}

func withInstallKind(t *testing.T, kind string) {
	t.Helper()
	oldFn := detectInstallKindFn
	detectInstallKindFn = func() string { return kind }
	installKindOnce = sync.Once{}
	t.Cleanup(func() {
		detectInstallKindFn = oldFn
		installKindOnce = sync.Once{}
	})
}

func cleanUpdateState(t *testing.T) {
	t.Helper()
	resetStatusForTest()
	SetDirective(nil)
	setDirectiveHook(nil)
	RegisterPrivilegedInstaller(nil)
	t.Cleanup(func() {
		resetStatusForTest()
		SetDirective(nil)
		setDirectiveHook(nil)
		RegisterPrivilegedInstaller(nil)
	})
}

// registryFake serves the signed lists and the archive of a release.
type registryFake struct {
	srv         *httptest.Server
	hits        atomic.Int32
	mu          sync.Mutex
	downloaded  []string
	stable      []byte
	stableSig   []byte
	beta        []byte
	betaSig     []byte
	archive     []byte
	archiveSig  []byte
	stableCode  int
	stableSigOK bool
}

func newRegistryFake(t *testing.T) *registryFake {
	t.Helper()
	f := &registryFake{stableCode: http.StatusOK}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		serve := func(code int, body []byte) {
			w.WriteHeader(code)
			_, _ = w.Write(body)
		}
		switch {
		case r.URL.Path == VERSION_METADATA_LIST_PATH:
			serve(f.stableCode, f.stable)
		case r.URL.Path == VERSION_METADATA_LIST_PATH+".minisig":
			serve(f.stableCode, f.stableSig)
		case r.URL.Path == VERSION_METADATA_LIST_BETA_PATH:
			serve(http.StatusOK, f.beta)
		case r.URL.Path == VERSION_METADATA_LIST_BETA_PATH+".minisig":
			serve(http.StatusOK, f.betaSig)
		case strings.HasSuffix(r.URL.Path, ".zip.minisig"):
			serve(http.StatusOK, f.archiveSig)
		case strings.HasSuffix(r.URL.Path, ".zip"):
			f.mu.Lock()
			f.downloaded = append(f.downloaded, r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/zip")
			serve(http.StatusOK, f.archive)
		default:
			serve(http.StatusNotFound, nil)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *registryFake) downloads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.downloaded...)
}

// useBenchLists serves the bench vectors; their key is injected.
func (f *registryFake) useBenchLists(t *testing.T) {
	withBenchKey(t)
	f.stable, f.stableSig = vector(t, "releases.json"), vector(t, "releases.json.minisig")
	f.beta, f.betaSig = vector(t, "beta-releases.json"), vector(t, "beta-releases.json.minisig")
}

// useOwnKey signs the given lists with a fresh key, which also signs the
// archive, so an update can be installed end to end.
func (f *registryFake) useOwnKey(t *testing.T, stable, beta []VersionMetadata) {
	priv := withSigningKey(t)
	sign := func(list []VersionMetadata) ([]byte, []byte) {
		if list == nil {
			return nil, nil
		}
		body, err := json.Marshal(list)
		if err != nil {
			t.Fatal(err)
		}
		return body, nil
	}
	f.stable, f.stableSig = sign(stable)
	f.beta, f.betaSig = sign(beta)
	signList := func(body []byte, channel string) []byte {
		r := minisign.NewReader(bytes.NewReader(body))
		_, _ = io.Copy(io.Discard, r)
		return r.SignWithComments(priv, "senhub-releases channel="+channel+" seq=1795000000", "test")
	}
	if f.stable != nil {
		f.stableSig = signList(f.stable, "stable")
	}
	if f.beta != nil {
		f.betaSig = signList(f.beta, "beta")
	}
	f.archive = releaseArchive(t, randomBinary(t, 1536*1024))
	f.archiveSig = prehashedSignature(t, priv, f.archive)
}

func list(versions ...string) []VersionMetadata {
	out := make([]VersionMetadata, 0, len(versions)+1)
	out = append(out, VersionMetadata{Name: "latest", Version: versions[0]})
	for _, v := range versions {
		out = append(out, VersionMetadata{Name: v, Version: v})
	}
	return out
}

type fleetUpdater struct {
	*autoUpdate
	target string
	cfg    *mockConfiguration
}

func (f *registryFake) updater(t *testing.T, goos string, fileEnabled bool) fleetUpdater {
	t.Helper()
	cfg := newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+f.srv.URL+`","update_enabled":`+boolJSON(fileEnabled)+`}}`)
	target := filepath.Join(t.TempDir(), "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	a := newTestAutoUpdate(cfg, f.srv.Client())
	a.targetPath = target
	a.stateDir = filepath.Join(t.TempDir(), "state")
	a.goos = goos
	zero := time.Duration(0)
	a.downloadWait = &zero
	a.listRetryWait = &zero
	return fleetUpdater{autoUpdate: a, target: target, cfg: cfg}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func wantStatus(t *testing.T, state, target, reason string) {
	t.Helper()
	got := CurrentStatus()
	if got.State != state || got.Target != target || got.Reason != reason {
		t.Fatalf("status = %+v, want {%s %s %s}", got, state, target, reason)
	}
}

func installed(t *testing.T, u fleetUpdater) bool {
	t.Helper()
	b, err := os.ReadFile(u.target)
	if err != nil {
		t.Fatal(err)
	}
	return !bytes.Equal(b, []byte("old binary"))
}

// Vector 1: update {auto:true} with no target, on a file that says
// enabled:false, installs the latest stable.
func TestFleetUpdate_AutoWithoutTargetInstallsLatest(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
	u := f.updater(t, "windows", false)

	SetDirective(&Directive{Auto: true})
	enabled, version := u.effectiveSettings()
	if !enabled || version != "latest" {
		t.Fatalf("effective settings = %v %q, want true latest", enabled, version)
	}
	applied, err := u.updateForDirective(*CurrentDirective(), f.srv.URL)
	if err != nil || !applied {
		t.Fatalf("updateForDirective = %v, %v; want an installed update", applied, err)
	}
	if !installed(t, u) {
		t.Error("the binary was not replaced")
	}
	if got := f.downloads(); len(got) != 1 || !strings.Contains(got[0], "/0.7.2/") {
		t.Errorf("downloads = %v, want only 0.7.2", got)
	}
	wantStatus(t, StateInstalling, "0.7.2", "")
}

// Vector 2: a pinned target holds the agent there when something newer is
// published.
func TestFleetUpdate_PinnedTargetStopsAtTarget(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.0")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1", "0.7.0"), nil)
	u := f.updater(t, "windows", false)

	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.1"}, f.srv.URL)
	if err != nil || !applied {
		t.Fatalf("updateForDirective = %v, %v", applied, err)
	}
	if got := f.downloads(); len(got) != 1 || !strings.Contains(got[0], "/0.7.1/") {
		t.Errorf("downloads = %v, want only 0.7.1", got)
	}
}

// Vector 3: auto:false makes no registry call at all.
func TestFleetUpdate_AutoFalseContactsNothing(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	u := f.updater(t, "windows", true)

	SetDirective(&Directive{Auto: false})
	if err := u.checkForUpdateOnce(); err != nil {
		t.Fatal(err)
	}
	if got := f.hits.Load(); got != 0 {
		t.Errorf("registry hit %d times with auto:false", got)
	}
	wantStatus(t, StateIdle, "", "")
}

// Vector 4: a lower target needs allow_downgrade; on an MSI install it is
// refused whatever the flag says.
func TestFleetUpdate_Downgrade(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		goos   string
		allow  bool
		reason string
		apply  bool
	}{
		{"without allow_downgrade", KindZip, "windows", false, CodeDowngradeNotAllowed, false},
		{"zip with allow_downgrade", KindZip, "windows", true, "", true},
		{"msi with allow_downgrade", KindMSI, "windows", true, CodeDowngradeMSI, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleanUpdateState(t)
			withRunningVersion(t, "0.7.2")
			withInstallKind(t, c.kind)
			f := newRegistryFake(t)
			f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
			u := f.updater(t, c.goos, true)

			applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.1", AllowDowngrade: c.allow}, f.srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if applied != c.apply {
				t.Fatalf("applied = %v, want %v", applied, c.apply)
			}
			if c.reason != "" {
				wantStatus(t, StateFailed, "0.7.1", c.reason)
				if installed(t, u) {
					t.Error("a refused downgrade replaced the binary")
				}
			}
		})
	}
}

// Vector 5.
func TestFleetUpdate_TargetNotPublished(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	u := f.updater(t, "windows", true)

	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.5"}, f.srv.URL)
	if err != nil || applied {
		t.Fatalf("updateForDirective = %v, %v", applied, err)
	}
	wantStatus(t, StateFailed, "0.7.5", CodeTargetNotPublished)
	if len(f.downloads()) != 0 {
		t.Error("an archive was downloaded for an unpublished target")
	}
}

// Vector 6, with the bench vectors: the good lists are used, every other one
// is refused.
func TestFleetUpdate_BenchVectors(t *testing.T) {
	withBenchKey(t)
	read := func(name string) ([]byte, []byte) {
		return vector(t, name), vector(t, strings.TrimSuffix(name, ".json")+".json.minisig")
	}
	cases := []struct {
		file    string
		channel string
		seq     int64
		wantErr string
	}{
		{"releases.json", "stable", 1795000000, ""},
		{"beta-releases.json", "beta", 1795000000, ""},
		{"old-seq-releases.json", "stable", 1794000000, ""},
		{"tampered-releases.json", "stable", 0, "verification failed"},
		{"wrong-channel-releases.json", "stable", 0, "signed for the beta channel"},
		{"no-comment-releases.json", "stable", 0, "missing or malformed"},
		{"malformed-comment-releases.json", "stable", 0, "missing or malformed"},
		{"releases.json", "beta", 0, "signed for the stable channel"},
	}
	for _, c := range cases {
		list, sig := read(c.file)
		seq, err := verifyListSignature(list, sig, c.channel)
		if c.wantErr == "" {
			if err != nil || seq != c.seq {
				t.Errorf("%s: seq %d, err %v; want seq %d", c.file, seq, err, c.seq)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err %v, want %q", c.file, err, c.wantErr)
		}
	}
	good, _ := read("releases.json")
	if _, err := verifyListSignature(good, vector(t, "beta-releases.json.minisig"), "stable"); err == nil {
		t.Fatal("a list verified with the signature of another list")
	}
}

// The seq floor is kept per channel, durably, and a fresh state accepts the
// first valid list.
func TestFleetUpdate_ListSeqFloor(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	u := f.updater(t, "windows", true)
	u.dryRun = true

	// A fresh state accepts the old list.
	f.stable, f.stableSig = vector(t, "old-seq-releases.json"), vector(t, "old-seq-releases.json.minisig")
	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.1"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := CurrentStatus(); got.State == StateFailed {
		t.Fatalf("a fresh state refused the first valid list: %+v", got)
	}

	// The newer list is accepted and raises the floor.
	f.stable, f.stableSig = vector(t, "releases.json"), vector(t, "releases.json.minisig")
	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateDownloading, "0.7.2", "")

	// The old list is now a replay, even from a new process on the same state.
	f.stable, f.stableSig = vector(t, "old-seq-releases.json"), vector(t, "old-seq-releases.json.minisig")
	u2 := f.updater(t, "windows", true)
	u2.stateDir = u.stateDir
	u2.dryRun = true
	if _, err := u2.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.1"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateFailed, "0.7.1", ReasonSignature)

	// The beta channel has its own floor.
	f.beta, f.betaSig = vector(t, "beta-releases.json"), vector(t, "beta-releases.json.minisig")
	if _, err := u2.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.3-beta.1"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateDownloading, "0.7.3-beta.1", "")
}

func TestFleetUpdate_BadCommentListsAreSignatureFailures(t *testing.T) {
	for _, name := range []string{"wrong-channel", "no-comment", "malformed-comment"} {
		t.Run(name, func(t *testing.T) {
			cleanUpdateState(t)
			withRunningVersion(t, "0.7.1")
			withInstallKind(t, KindZip)
			f := newRegistryFake(t)
			f.useBenchLists(t)
			f.stable, f.stableSig = vector(t, name+"-releases.json"), vector(t, name+"-releases.json.minisig")
			u := f.updater(t, "windows", true)
			if _, err := u.updateForDirective(Directive{Auto: true}, f.srv.URL); err != nil {
				t.Fatal(err)
			}
			wantStatus(t, StateFailed, "", ReasonSignature)
			if len(f.downloads()) != 0 {
				t.Error("an archive was downloaded after a refused list")
			}
		})
	}
}

func TestFleetUpdate_TamperedListIsASignatureFailure(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	f.stable, f.stableSig = vector(t, "tampered-releases.json"), vector(t, "tampered-releases.json.minisig")
	u := f.updater(t, "windows", true)

	applied, err := u.updateForDirective(Directive{Auto: true}, f.srv.URL)
	if err != nil || applied {
		t.Fatalf("updateForDirective = %v, %v", applied, err)
	}
	wantStatus(t, StateFailed, "", ReasonSignature)
	if len(f.downloads()) != 0 {
		t.Error("an archive was downloaded after a bad list")
	}
}

func TestFleetUpdate_UnreachableListIsASignatureFailure(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	f.stableCode = http.StatusServiceUnavailable
	u := f.updater(t, "windows", true)

	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateFailed, "0.7.2", ReasonSignature)
}

func TestFleetUpdate_ListSignedByAnotherKeyIsRefused(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	withSigningKey(t) // the agent now pins a key that did not sign the lists
	u := f.updater(t, "windows", true)

	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateFailed, "0.7.2", ReasonSignature)
}

func TestFleetUpdate_BuildWithoutKeyRefuses(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	signingPublicKey = ""
	u := f.updater(t, "windows", true)

	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateFailed, "0.7.2", ReasonSignature)
}

// A beta target is looked up in the signed beta list, include_beta or not,
// and is refused on an MSI install.
func TestFleetUpdate_BetaTarget(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.2")
	f := newRegistryFake(t)
	f.useBenchLists(t)

	t.Run("published in the beta list", func(t *testing.T) {
		cleanUpdateState(t)
		withInstallKind(t, KindZip)
		u := f.updater(t, "windows", true)
		u.cfg.SetConfiguration(withIncludeBeta(u.cfg.GetConfiguration(), false))
		u.dryRun = true
		applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.3-beta.1"}, f.srv.URL)
		if err != nil || !applied {
			t.Fatalf("updateForDirective = %v, %v", applied, err)
		}
		wantStatus(t, StateDownloading, "0.7.3-beta.1", "")
	})
	t.Run("not in the beta list", func(t *testing.T) {
		cleanUpdateState(t)
		withInstallKind(t, KindZip)
		u := f.updater(t, "windows", true)
		if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.3-beta.2"}, f.srv.URL); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, StateFailed, "0.7.3-beta.2", CodeTargetNotPublished)
	})
	t.Run("msi", func(t *testing.T) {
		cleanUpdateState(t)
		withInstallKind(t, KindMSI)
		u := f.updater(t, "windows", true)
		if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.3-beta.1"}, f.srv.URL); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, StateFailed, "0.7.3-beta.1", CodeBetaMSI)
	})
}

func TestFleetUpdate_LatestJoinsTheBetaListOnlyWithIncludeBeta(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.2")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useBenchLists(t)
	u := f.updater(t, "windows", true)
	u.dryRun = true

	if applied, _ := u.updateForDirective(Directive{Auto: true}, f.srv.URL); applied {
		t.Fatal("a stable channel agent moved to a beta")
	}
	u.cfg.SetConfiguration(withIncludeBeta(u.cfg.GetConfiguration(), true))
	if applied, err := u.updateForDirective(Directive{Auto: true}, f.srv.URL); err != nil || !applied {
		t.Fatalf("an include_beta agent did not take the beta: %v, %v", applied, err)
	}
	if u.resolved != "0.7.3-beta.1" {
		t.Errorf("resolved = %q", u.resolved)
	}
}

// Vector 11: a target that failed to start is refused, with the memory of
// the rollback, until retry lifts it.
func TestFleetUpdate_RolledBackTarget(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
	u := f.updater(t, "windows", true)
	env := u.rollbackEnvFor(u.target)
	if err := env.writeRolledBackRecord(RolledBackRecord{FailedVersion: "0.7.2", RestoredVersion: "0.7.1", Outcome: OutcomeRestored}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL)
		if err != nil || applied {
			t.Fatalf("push %d: %v, %v", i, applied, err)
		}
		wantStatus(t, StateRolledBack, "0.7.2", "rolled_back:0.7.2")
	}
	if len(f.downloads()) != 0 {
		t.Fatal("a rolled-back target was downloaded without retry")
	}

	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2", Retry: true}, f.srv.URL)
	if err != nil || !applied {
		t.Fatalf("with retry: %v, %v", applied, err)
	}
	if len(f.downloads()) != 1 {
		t.Errorf("downloads = %v", f.downloads())
	}
}

func TestFleetUpdate_RolledBackMemoryIsPerVersion(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.3", "0.7.2", "0.7.1"), nil)
	u := f.updater(t, "windows", true)
	if err := u.rollbackEnvFor(u.target).writeRolledBackRecord(RolledBackRecord{FailedVersion: "0.7.2", RestoredVersion: "0.7.1", Outcome: OutcomeRestored}); err != nil {
		t.Fatal(err)
	}
	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.3"}, f.srv.URL)
	if err != nil || !applied {
		t.Fatalf("another target after a rollback: %v, %v", applied, err)
	}
}

// After the restart that follows a rollback the agent reports it, from the
// record the boot guard wrote.
func TestBaselineStatus_RolledBackAfterRestart(t *testing.T) {
	withRunningVersion(t, "0.7.1")
	dir := t.TempDir()
	if got := baselineStatus(dir); got.State != StateIdle {
		t.Fatalf("without a record: %+v", got)
	}
	env := newRollbackEnv(dir, filepath.Join(dir, "senhub-agent"), createTestModuleLogger())
	if err := env.writeRolledBackRecord(RolledBackRecord{FailedVersion: "0.7.2", RestoredVersion: "0.7.1", Outcome: OutcomeRestored}); err != nil {
		t.Fatal(err)
	}
	got := baselineStatus(dir)
	if got != (Status{State: StateRolledBack, Target: "0.7.2", Reason: "rolled_back:0.7.2"}) {
		t.Fatalf("baseline = %+v", got)
	}
	withRunningVersion(t, "0.7.3")
	if got := baselineStatus(dir); got.State != StateIdle {
		t.Fatalf("once another version runs, baseline = %+v", got)
	}
}

// Vector 12: a Linux install the service cannot replace answers
// not_replaceable, unless a privileged path is registered.
func TestFleetUpdate_NotReplaceableOnLinux(t *testing.T) {
	for _, kind := range []string{KindDeb, KindRPM, KindTarball} {
		t.Run(kind, func(t *testing.T) {
			cleanUpdateState(t)
			withRunningVersion(t, "0.7.1")
			withInstallKind(t, kind)
			f := newRegistryFake(t)
			f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
			u := f.updater(t, "linux", true)

			applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL)
			if err != nil || applied {
				t.Fatalf("updateForDirective = %v, %v", applied, err)
			}
			wantStatus(t, StateFailed, "0.7.2", ReasonNotReplace)
			if len(f.downloads()) != 0 || installed(t, u) {
				t.Error("something was installed on a non replaceable install")
			}
		})
	}
}

type fakePrivileged struct {
	available bool
	asked     []string
}

func (p *fakePrivileged) Available() bool { return p.available }
func (p *fakePrivileged) Request(v string) error {
	p.asked = append(p.asked, v)
	return nil
}

func TestFleetUpdate_PrivilegedPathMakesAPackageInstallReplaceable(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindDeb)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
	u := f.updater(t, "linux", true)
	p := &fakePrivileged{available: true}
	RegisterPrivilegedInstaller(p)

	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL)
	if err != nil || applied {
		t.Fatalf("updateForDirective = %v, %v", applied, err)
	}
	if len(p.asked) != 1 || p.asked[0] != "0.7.2" {
		t.Errorf("privileged requests = %v", p.asked)
	}
	wantStatus(t, StateInstalling, "0.7.2", "")

	p.available = false
	if _, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, StateFailed, "0.7.2", ReasonNotReplace)
}

func TestCanReplaceSelf(t *testing.T) {
	cleanUpdateState(t)
	cases := []struct {
		goos, kind string
		want       bool
	}{
		{"windows", KindMSI, true},
		{"windows", KindZip, true},
		{"linux", KindMSI, true},
		{"linux", KindDeb, false},
		{"linux", KindRPM, false},
		{"linux", KindTarball, false},
		{"linux", KindUnknown, false},
		{"darwin", KindUnknown, true},
	}
	for _, c := range cases {
		if got := canReplaceSelf(c.goos, c.kind); got != c.want {
			t.Errorf("canReplaceSelf(%s, %s) = %v, want %v", c.goos, c.kind, got, c.want)
		}
	}
}

// Application at once: the directive reaches a running updater, which checks
// the registry without waiting for the hourly tick; removing it brings the
// local file back, without a restart.
func TestFleetUpdate_DirectiveChecksImmediatelyAndRemovalRestoresTheFile(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	srv, calls := newCountingRegistry(t)
	cfg := newMockConfiguration("", `{"agent":{"version":"0.7.0","registry_url":"`+srv.URL+`","update_enabled":false}}`)
	a := newTestAutoUpdate(cfg, srv.Client())
	a.stateDir = t.TempDir()
	setDirectiveHook(a.checkAfterDirective)

	SetDirective(&Directive{Auto: true, TargetVersion: "0.7.2"})
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("no registry call within a second of the directive")
	}

	enabled, version := a.effectiveSettings()
	if !enabled || version != "0.7.2" {
		t.Fatalf("with the directive: %v %q", enabled, version)
	}
	SetDirective(nil)
	enabled, version = a.effectiveSettings()
	if enabled || version != "0.7.0" {
		t.Fatalf("after removal the file must rule: %v %q", enabled, version)
	}
}

func TestSetDirective_UnchangedDoesNotRecheck(t *testing.T) {
	cleanUpdateState(t)
	var n atomic.Int32
	setDirectiveHook(func() { n.Add(1) })
	SetDirective(&Directive{Auto: true})
	SetDirective(&Directive{Auto: true})
	time.Sleep(50 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Errorf("hook ran %d times for one change", got)
	}
	SetDirective(nil)
	SetDirective(nil)
	time.Sleep(50 * time.Millisecond)
	if got := n.Load(); got != 2 {
		t.Errorf("hook ran %d times after a removal, want 2", got)
	}
}

// Autonomous mode keeps its rule with no directive.
func TestEffectiveSettings_NoDirectiveReadsTheFile(t *testing.T) {
	cleanUpdateState(t)
	cfg := newMockConfiguration("", `{"agent":{"version":"0.7.0","update_enabled":true}}`)
	a := newTestAutoUpdate(cfg, http.DefaultClient)
	enabled, version := a.effectiveSettings()
	if !enabled || version != "0.7.0" {
		t.Fatalf("got %v %q", enabled, version)
	}
}

func TestStatusChangeNotifies(t *testing.T) {
	cleanUpdateState(t)
	got := make(chan struct{}, 4)
	OnStatusChange(func() { got <- struct{}{} })
	setStatus(Status{State: StateChecking})
	setStatus(Status{State: StateChecking})
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
	select {
	case <-got:
		t.Fatal("an unchanged status notified twice")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestInstallKindFor(t *testing.T) {
	owner := func(v string) func(string) string { return func(string) string { return v } }
	cases := []struct {
		goos string
		msi  bool
		exe  string
		own  string
		want string
	}{
		{"windows", true, `C:\Program Files\SenHub\senhub-agent.exe`, "", KindMSI},
		{"windows", false, `C:\SenHub\senhub-agent.exe`, "", KindZip},
		{"linux", false, "/usr/bin/senhub-agent", "deb", KindDeb},
		{"linux", false, "/usr/bin/senhub-agent", "rpm", KindRPM},
		{"linux", false, "/opt/senhub/senhub-agent", "", KindTarball},
		{"linux", false, "", "", KindUnknown},
		{"darwin", false, "/usr/local/bin/senhub-agent", "", KindUnknown},
	}
	for _, c := range cases {
		if got := installKindFor(c.goos, c.msi, c.exe, owner(c.own)); got != c.want {
			t.Errorf("installKindFor(%s, msi=%v, %q, %q) = %q, want %q", c.goos, c.msi, c.exe, c.own, got, c.want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	order := []string{
		"0.7.2-alpha", "0.7.2-alpha.1", "0.7.2-alpha.beta", "0.7.2-beta", "0.7.2-beta.2",
		"0.7.2-beta.11", "0.7.2-rc.1", "0.7.2", "0.7.10", "0.10.0", "1.0.0",
	}
	for i, a := range order {
		for j, b := range order {
			got, err := compareSemver(a, b)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got != want {
				t.Errorf("compareSemver(%s, %s) = %d, want %d", a, b, got, want)
			}
		}
	}
	if c, _ := compareSemver("0.7.2+build.5", "0.7.2"); c != 0 {
		t.Errorf("build metadata must not count, got %d", c)
	}
	if c, _ := compareSemver("v0.7.2", "0.7.2"); c != 0 {
		t.Errorf("a leading v must not count, got %d", c)
	}
	for _, bad := range []string{"", "latest", "0.7", "01.2.3", "0.7.2-", ">=0.7"} {
		if _, err := compareSemver(bad, "0.7.2"); err == nil {
			t.Errorf("%q parsed as a version", bad)
		}
	}
}

func TestSignedListCarriesTheRegistryPaths(t *testing.T) {
	if VERSION_METADATA_LIST_PATH != "/releases/releases.json" || VERSION_METADATA_LIST_BETA_PATH != "/releases/beta/releases.json" {
		t.Fatal("the contract names these two paths; their signatures sit next to them")
	}
}

func withIncludeBeta(d configuration.ConfigurationData, on bool) configuration.ConfigurationData {
	d.Agent.IncludeBeta = on
	return d
}

// An MSI install takes a newer version through the MSI hand-over, never the
// archive path.
func TestFleetUpdate_MSIInstallUsesTheMSIHandOver(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindMSI)
	f := newRegistryFake(t)
	f.useOwnKey(t, list("0.7.2", "0.7.1"), nil)
	u := f.updater(t, "windows", true)
	var gotURL, gotVersion string
	u.installMSI = func(msiURL, version string) error {
		gotURL, gotVersion = msiURL, version
		return nil
	}

	applied, err := u.updateForDirective(Directive{Auto: true, TargetVersion: "0.7.2"}, f.srv.URL)
	if err != nil || !applied {
		t.Fatalf("updateForDirective = %v, %v", applied, err)
	}
	if gotVersion != "0.7.2" || !strings.HasSuffix(gotURL, ".msi") {
		t.Errorf("hand-over got %q %q", gotURL, gotVersion)
	}
	if len(f.downloads()) != 0 {
		t.Error("the archive path ran on an MSI install")
	}
	wantStatus(t, StateInstalling, "0.7.2", "")
}

func TestFailureReasonSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("outer: %w", fail(ReasonSignature, errors.New("bad")))
	if got := failureReason(err); got != ReasonSignature {
		t.Errorf("reason = %q", got)
	}
	if got := failureReason(errors.New("plain")); got != ReasonInstall {
		t.Errorf("reason = %q, want the default", got)
	}
}

// The list and its signature come from two requests: a pair split by a
// publication in between is fetched again, and refused only if it stays so.
func TestFleetUpdate_MismatchedPairIsFetchedAgain(t *testing.T) {
	cleanUpdateState(t)
	withRunningVersion(t, "0.7.1")
	withInstallKind(t, KindZip)
	withBenchKey(t)
	good, goodSig := vector(t, "releases.json"), vector(t, "releases.json.minisig")
	otherSig := vector(t, "old-seq-releases.json.minisig")

	serve := func(mismatches int32) (*httptest.Server, *atomic.Int32) {
		var listCalls atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case VERSION_METADATA_LIST_PATH:
				listCalls.Add(1)
				w.Header().Set(manifestSeqHeader, "2")
				_, _ = w.Write(good)
			case VERSION_METADATA_LIST_PATH + ".minisig":
				if listCalls.Load() <= mismatches {
					w.Header().Set(manifestSeqHeader, "3")
					_, _ = w.Write(otherSig)
					return
				}
				w.Header().Set(manifestSeqHeader, "2")
				_, _ = w.Write(goodSig)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)
		return srv, &listCalls
	}
	updater := func(srv *httptest.Server) *autoUpdate {
		cfg := newMockConfiguration("", `{"agent":{"version":"latest","registry_url":"`+srv.URL+`","update_enabled":true}}`)
		a := newTestAutoUpdate(cfg, srv.Client())
		a.stateDir = t.TempDir()
		zero := time.Duration(0)
		a.listRetryWait = &zero
		a.dryRun = true
		return a
	}

	srv, calls := serve(1)
	a := updater(srv)
	if _, err := a.fetchSignedList(srv.URL, VERSION_METADATA_LIST_PATH); err != nil {
		t.Fatalf("a pair split once must be accepted on the next fetch: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("list fetched %d times, want 2", got)
	}

	srv, calls = serve(100)
	a = updater(srv)
	if _, err := a.fetchSignedList(srv.URL, VERSION_METADATA_LIST_PATH); err == nil {
		t.Fatal("a pair that never matches was accepted")
	}
	if got := calls.Load(); got != listFetchAttempts {
		t.Errorf("list fetched %d times, want %d", got, listFetchAttempts)
	}

	// Equal headers but a signature that fails is retried too, then refused.
	bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(manifestSeqHeader, "2")
		if strings.HasSuffix(r.URL.Path, ".minisig") {
			_, _ = w.Write(otherSig)
			return
		}
		_, _ = w.Write(good)
	}))
	t.Cleanup(bad.Close)
	b := updater(bad)
	if _, err := b.fetchSignedList(bad.URL, VERSION_METADATA_LIST_PATH); err == nil {
		t.Fatal("a list with the signature of another was accepted")
	}
}
