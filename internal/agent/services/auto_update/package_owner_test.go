package auto_update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageOwner(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	exe, _ = filepath.EvalSymlinks(exe)
	info := filepath.Join(dir, "dpkg-info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	noRPM := func(string) bool { return false }

	if got := packageOwner(exe, info, noRPM); got != "" {
		t.Errorf("archive install: owner = %q, want none", got)
	}
	if err := os.WriteFile(filepath.Join(info, "senhub-agent-oss.list"), []byte("/.\n/usr\n"+exe+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := packageOwner(exe, info, noRPM); got != "deb" {
		t.Errorf("deb install: owner = %q, want deb", got)
	}
	if got := packageOwner(exe, t.TempDir(), func(string) bool { return true }); got != "rpm" {
		t.Errorf("rpm install: owner = %q, want rpm", got)
	}
}
