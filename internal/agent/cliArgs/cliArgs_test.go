package cliArgs

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGetAbsoluteConfigPath_EmptyReturnsCanonical confirms the
// zero-argument path resolution returns the OS-canonical location
// (see paths_<goos>.go for the per-OS string).
func TestGetAbsoluteConfigPath_EmptyReturnsCanonical(t *testing.T) {
	got, err := GetAbsoluteConfigPath("")
	if err != nil {
		t.Fatalf("GetAbsoluteConfigPath(\"\") returned error: %v", err)
	}
	if got == "" {
		t.Fatal("GetAbsoluteConfigPath(\"\") returned empty string")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("GetAbsoluteConfigPath(\"\") returned non-absolute path: %q", got)
	}
	switch runtime.GOOS {
	case "linux":
		if got != "/etc/senhub-agent/agent.yaml" {
			t.Errorf("linux canonical = %q; want /etc/senhub-agent/agent.yaml", got)
		}
	case "darwin":
		if got != "/usr/local/etc/senhub-agent/agent.yaml" {
			t.Errorf("darwin canonical = %q; want /usr/local/etc/senhub-agent/agent.yaml", got)
		}
	case "windows":
		if !strings.HasSuffix(got, `SenHub\agent.yaml`) {
			t.Errorf("windows canonical = %q; want suffix SenHub\\agent.yaml", got)
		}
	}
}

func TestGetAbsoluteConfigPath_AbsoluteReturnedAsIs(t *testing.T) {
	in := "/tmp/custom-config.yaml"
	if runtime.GOOS == "windows" {
		in = `C:\Temp\custom-config.yaml`
	}
	got, err := GetAbsoluteConfigPath(in)
	if err != nil {
		t.Fatalf("GetAbsoluteConfigPath(%q) returned error: %v", in, err)
	}
	if got != filepath.Clean(in) {
		t.Errorf("GetAbsoluteConfigPath(%q) = %q; want %q", in, got, filepath.Clean(in))
	}
}

func TestGetAbsoluteConfigPath_RejectsTraversal(t *testing.T) {
	_, err := GetAbsoluteConfigPath("../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestPrintVersion_DoesNotPanic(t *testing.T) {
	// Defensive smoke: PrintVersion writes to stdout; this test just
	// makes sure it doesn't panic on common combinations of the
	// build-injected globals.
	saveVer, saveCommit, saveEnv := Version, CommitHash, Env
	defer func() { Version, CommitHash, Env = saveVer, saveCommit, saveEnv }()

	for _, tc := range []struct{ ver, commit, env string }{
		{"0.2.0", "abc123", "production"},
		{"0.2.0-beta", "", "production"},
		{"", "abc123", "production"},
		{"", "", "production"},
		{"0.2.0", "abc", "development"},
	} {
		Version, CommitHash, Env = tc.ver, tc.commit, tc.env
		PrintVersion()
	}
}
