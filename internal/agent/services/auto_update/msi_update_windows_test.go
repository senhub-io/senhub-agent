//go:build windows

package auto_update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"aead.dev/minisign"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// msiFixtureBody returns a body comfortably above minMSISize so the size guards
// pass and verification/staging is what's exercised.
func msiFixtureBody() []byte {
	b := make([]byte, minMSISize+4096)
	for i := range b {
		b[i] = byte(i * 5 % 251)
	}
	return b
}

// serveMSI serves the MSI at /dl/agent.msi and, when withSig, its detached
// signature at /dl/agent.msi.minisig over TLS.
func serveMSI(body, sig []byte, withSig bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/agent.msi", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	})
	if withSig {
		mux.HandleFunc("/dl/agent.msi.minisig", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(sig)
		})
	}
	return httptest.NewTLSServer(mux)
}

// TestApplyMsiUpdate_LaunchesAfterVerify pins the verify-BEFORE-launch ordering
// and the on-success bookkeeping: the staged file handed to the launcher must be
// the verified body, msiUpgradeLaunched is set (so the caller does not self-exit
// into the installer), lastMsiAttempt records the version, and the staging dir
// is kept (msiexec reads it back detached).
func TestApplyMsiUpdate_LaunchesAfterVerify(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	agentstate.ResetUpdateRejectedForTest()
	t.Cleanup(agentstate.ResetUpdateRejectedForTest)

	priv := withSigningKey(t)
	body := msiFixtureBody()
	sig := minisign.Sign(priv, body)
	srv := serveMSI(body, sig, true)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	var launched atomic.Int32
	var launchedPath string
	au.launchInstaller = func(msiPath, _ string) error {
		got, err := os.ReadFile(msiPath)
		if err != nil {
			t.Errorf("staged MSI unreadable at launch: %v", err)
		}
		if len(got) != len(body) {
			t.Errorf("staged MSI size = %d, want %d (verify must precede launch)", len(got), len(body))
		}
		launchedPath = msiPath
		launched.Add(1)
		return nil
	}

	if err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9"); err != nil {
		t.Fatalf("applyMsiUpdate: %v", err)
	}
	if launched.Load() != 1 {
		t.Fatalf("installer launched %d times, want 1", launched.Load())
	}
	if !au.msiUpgradeLaunched.Load() {
		t.Error("msiUpgradeLaunched not set after launch")
	}
	if att := au.lastMsiAttempt.Load(); att == nil || att.version != "9.9.9" {
		t.Errorf("lastMsiAttempt = %+v, want version 9.9.9", att)
	}
	if _, err := os.Stat(launchedPath); err != nil {
		t.Errorf("staged MSI removed after a successful launch: %v", err)
	}
}

// TestApplyMsiUpdate_DryRunDoesNotLaunch pins M1: --dry-run must never hand the
// upgrade to msiexec.
func TestApplyMsiUpdate_DryRunDoesNotLaunch(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())

	priv := withSigningKey(t)
	body := msiFixtureBody()
	sig := minisign.Sign(priv, body)
	srv := serveMSI(body, sig, true)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	au.dryRun = true
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	if err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9"); err != nil {
		t.Fatalf("dry-run applyMsiUpdate returned error: %v", err)
	}
	if launched {
		t.Error("dry-run launched msiexec")
	}
	if au.msiUpgradeLaunched.Load() {
		t.Error("dry-run set msiUpgradeLaunched")
	}
}

// TestApplyMsiUpdate_RejectsNonHTTPS pins m1: a plaintext registry URL is refused
// before any download.
func TestApplyMsiUpdate_RejectsNonHTTPS(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	srv := httptest.NewServer(http.NewServeMux()) // plain http
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS rejection, got %v", err)
	}
	if launched {
		t.Error("launched despite a non-HTTPS URL")
	}
}

// TestApplyMsiUpdate_TamperedRejectedWithMetric pins m3: a tampered MSI is
// refused and feeds the update_rejected self-metric, and is never launched.
func TestApplyMsiUpdate_TamperedRejectedWithMetric(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	agentstate.ResetUpdateRejectedForTest()
	t.Cleanup(agentstate.ResetUpdateRejectedForTest)

	priv := withSigningKey(t)
	body := msiFixtureBody()
	sig := minisign.Sign(priv, body)
	tampered := append([]byte{}, body...)
	tampered[len(tampered)/2] ^= 0xFF
	srv := serveMSI(tampered, sig, true)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "REJECTED") {
		t.Fatalf("expected REJECTED for tampered MSI, got %v", err)
	}
	if launched {
		t.Error("launched a tampered MSI")
	}
	if got := agentstate.GetUpdateRejectedByReason()["signature_invalid"]; got != 1 {
		t.Errorf("signature_invalid counter = %d, want 1", got)
	}
}

// TestApplyMsiUpdate_MissingSignatureRejectedWithMetric pins m3 for the
// unavailable-signature branch.
func TestApplyMsiUpdate_MissingSignatureRejectedWithMetric(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	agentstate.ResetUpdateRejectedForTest()
	t.Cleanup(agentstate.ResetUpdateRejectedForTest)

	withSigningKey(t)
	body := msiFixtureBody()
	srv := serveMSI(body, nil, false) // no .minisig route
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9")
	if err == nil {
		t.Fatal("MSI with no published signature was accepted")
	}
	if launched {
		t.Error("launched an unsigned MSI")
	}
	if got := agentstate.GetUpdateRejectedByReason()["signature_unavailable"]; got != 1 {
		t.Errorf("signature_unavailable counter = %d, want 1", got)
	}
}

// TestApplyMsiUpdate_RejectsHTMLLandingPage pins m2: the release server's 200
// HTML answer for an unknown path is caught by the content-type guard, not
// surfaced later as a misleading signature failure.
func TestApplyMsiUpdate_RejectsHTMLLandingPage(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("/dl/agent.msi", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not found</html>"))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("expected content-type rejection, got %v", err)
	}
	if launched {
		t.Error("launched on an HTML landing page")
	}
}

// TestApplyMsiUpdate_RejectsTooSmall pins m2's minimum-size guard.
func TestApplyMsiUpdate_RejectsTooSmall(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("/dl/agent.msi", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("tiny"))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	au := newSigningTestUpdater(t, srv)
	launched := false
	au.launchInstaller = func(_, _ string) error { launched = true; return nil }

	err := au.applyMsiUpdate(srv.URL+"/dl/agent.msi", "9.9.9")
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("expected too-small rejection, got %v", err)
	}
	if launched {
		t.Error("launched on an undersized body")
	}
}
