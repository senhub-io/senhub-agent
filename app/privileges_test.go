package app

import (
	"os/user"
	"runtime"
	"testing"
)

func TestLinuxCommandNeedsRoot(t *testing.T) {
	rootCommands := []string{"install", "uninstall", "start", "stop", "restart", "refresh-unit"}
	for _, cmd := range rootCommands {
		if !linuxCommandNeedsRoot(cmd) {
			t.Errorf("linuxCommandNeedsRoot(%q) = false, want true (service lifecycle)", cmd)
		}
	}

	// The daemon and inspection commands must NOT require root — this is
	// the core of #223: the long-running agent runs least-privilege.
	nonRootCommands := []string{"run", "status", "version", "config", "update", "license", "db-monitoring", ""}
	for _, cmd := range nonRootCommands {
		if linuxCommandNeedsRoot(cmd) {
			t.Errorf("linuxCommandNeedsRoot(%q) = true, want false (no elevation needed)", cmd)
		}
	}
}

// TestCheckPrivilegesRunNonRoot asserts that the daemon path is not
// gated behind root on Linux. This is the regression guard for #223:
// the shipped systemd unit runs `User=senhub`, so `run` must succeed
// for a non-root UID.
func TestCheckPrivilegesRunNonRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Linux-specific privilege semantics; GOOS=%s", runtime.GOOS)
	}
	if err := checkPrivileges("run"); err != nil {
		t.Errorf("checkPrivileges(\"run\") = %v, want nil (run must not require root)", err)
	}
}

// TestCheckPrivilegesInstallRequiresRoot asserts service-lifecycle
// commands keep their root requirement on Linux. Only meaningful when
// the test itself runs unprivileged.
func TestCheckPrivilegesInstallRequiresRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Linux-specific privilege semantics; GOOS=%s", runtime.GOOS)
	}
	cur, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	if cur.Uid == "0" {
		t.Skip("running as root; cannot assert the non-root rejection path")
	}
	if err := checkPrivileges("install"); err == nil {
		t.Error("checkPrivileges(\"install\") = nil as non-root, want an error (service management needs root)")
	}
}
