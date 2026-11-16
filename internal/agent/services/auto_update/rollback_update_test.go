package auto_update

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

func TestDoUpdate_KeepsThePreviousBinaryAndRecordsTheSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rename dance hides the old file on Windows; covered by the MSI path there")
	}
	priv := withSigningKey(t)
	binary := randomBinary(t, 1536*1024)
	archive := releaseArchive(t, binary)
	srv := releaseServer(t, archive, prehashedSignature(t, priv, archive))

	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	u := updaterFor(t, srv, target)
	u.stateDir = filepath.Join(t.TempDir(), "state")

	if err := u.doUpdate(srv.URL+"/senhub-agent.zip", "9.9.9"); err != nil {
		t.Fatalf("doUpdate: %v", err)
	}

	prev, err := os.ReadFile(target + ".previous")
	if err != nil {
		t.Fatalf("previous binary not kept: %v", err)
	}
	if string(prev) != "old binary" {
		t.Errorf("previous binary = %q, want the replaced one", prev)
	}
	if info, _ := os.Stat(target + ".previous"); info.Mode().Perm() != 0o750 {
		t.Errorf("previous mode = %v, want 0750", info.Mode().Perm())
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, binary) {
		t.Error("target does not hold the new binary")
	}

	marker, err := os.ReadFile(filepath.Join(u.stateDir, pendingFileName))
	if err != nil {
		t.Fatalf("pending marker not written: %v", err)
	}
	for _, want := range []string{`"from_version":"` + cliArgs.Version + `"`, `"to_version":"9.9.9"`, `"boot_attempts":0`} {
		if !bytes.Contains(marker, []byte(want)) {
			t.Errorf("marker %s lacks %s", marker, want)
		}
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("files left next to the binary: %v", left)
	}
}

func TestDoUpdate_WithoutAVersionLeavesNoRollbackState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rename dance hides the old file on Windows; covered by the MSI path there")
	}
	priv := withSigningKey(t)
	archive := releaseArchive(t, randomBinary(t, 1536*1024))
	srv := releaseServer(t, archive, prehashedSignature(t, priv, archive))

	target := filepath.Join(t.TempDir(), "senhub-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	u := updaterFor(t, srv, target)
	u.stateDir = filepath.Join(t.TempDir(), "state")

	if err := u.doUpdate(srv.URL+"/senhub-agent.zip", ""); err != nil {
		t.Fatalf("doUpdate: %v", err)
	}
	if _, err := os.Stat(target + ".previous"); !os.IsNotExist(err) {
		t.Errorf("a previous binary was kept without a target version: %v", err)
	}
}
