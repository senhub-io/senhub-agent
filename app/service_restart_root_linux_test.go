//go:build linux

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The root update unit must never run, nor even look at, a path the service
// can name. The restarter's executable hook returns the fixed binary whatever
// the process id says, and the version line comes from that binary only.
func TestRootServiceRestarterNeverUsesAProcessProvidedPath(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, "planted")
	ran := filepath.Join(dir, "ran")
	script := "#!/bin/sh\ntouch " + ran + "\n"
	if err := os.WriteFile(planted, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// A fake process entry whose command line names the planted script.
	proc := filepath.Join(dir, "proc", "4242")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "cmdline"), []byte(planted+"\x00run\x00"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixed := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(fixed, []byte("#!/bin/sh\necho 'Version: 9.9.9'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRootServiceRestarter(true, fixed)
	for _, pid := range []string{"4242", filepath.Join(dir, "proc", "4242"), "../../" + planted} {
		exe, err := r.exePath(pid)
		if err != nil || exe != fixed {
			t.Fatalf("exePath(%q) = %q, %v; want the fixed binary", pid, exe, err)
		}
	}
	if got := r.version(fixed); got != "Version: 9.9.9" {
		t.Errorf("version = %q", got)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("the planted script ran")
	}
}
