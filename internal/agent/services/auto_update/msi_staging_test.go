package auto_update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSecureStageDir_UnpredictableAndScoped pins the anti-TOCTOU staging: each
// call must return a fresh, distinct directory under the given base. The pre-fix
// code staged the MSI at a single fixed path in the shared temp dir, which an
// attacker can pre-place; a fixed path makes both calls collide and fails here.
func TestSecureStageDir_UnpredictableAndScoped(t *testing.T) {
	base := t.TempDir()

	d1, err := secureStageDir(base)
	if err != nil {
		t.Fatalf("secureStageDir: %v", err)
	}
	d2, err := secureStageDir(base)
	if err != nil {
		t.Fatalf("secureStageDir: %v", err)
	}

	if d1 == d2 {
		t.Errorf("two stagings returned the same path %q; a fixed path lets an attacker pre-place a file", d1)
	}
	for _, d := range []string{d1, d2} {
		if filepath.Dir(d) != base {
			t.Errorf("staging dir %q is not under base %q", d, base)
		}
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			t.Errorf("staging dir %q was not created as a directory: %v", d, err)
		}
	}
}

// TestWriteStagedFile_RefusesClobber pins the O_EXCL guarantee: an entry already
// present at the target (an attacker-planted file or symlink) must not be
// followed or overwritten, so nothing unverified can reach msiexec. The pre-fix
// os.WriteFile clobbers it and this test fails on that code.
func TestWriteStagedFile_RefusesClobber(t *testing.T) {
	dir := t.TempDir()

	if _, err := writeStagedFile(dir, "u.msi", []byte("verified")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writeStagedFile(dir, "u.msi", []byte("attacker")); err == nil {
		t.Errorf("writeStagedFile overwrote an existing path; O_EXCL must refuse it")
	}

	got, err := os.ReadFile(filepath.Join(dir, "u.msi"))
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if string(got) != "verified" {
		t.Errorf("staged file was overwritten: got %q, want %q", got, "verified")
	}
}
