package auto_update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveUpdateLeftoversDeletesOnlyTheFilesAnUpdateLeaves(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "senhub-agent.exe")
	for _, name := range []string{"senhub-agent.exe", ".senhub-agent.exe.old", ".senhub-agent.exe.new", "senhub-console.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removed := removeUpdateLeftovers(target)
	if len(removed) != 2 {
		t.Fatalf("removed %v, want the .old and .new files", removed)
	}
	for _, name := range []string{".senhub-agent.exe.old", ".senhub-agent.exe.new"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists", name)
		}
	}
	for _, name := range []string{"senhub-agent.exe", "senhub-console.exe"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
	if again := removeUpdateLeftovers(target); len(again) != 0 {
		t.Errorf("second pass removed %v", again)
	}
}
