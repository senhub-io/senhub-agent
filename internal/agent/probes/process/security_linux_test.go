//go:build linux

package process

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAllocatedFileHandlesIsTheFirstFieldOfFileNr(t *testing.T) {
	p := writeTemp(t, "file-nr", "3552\t0\t9223372036854775807\n")
	got, err := allocatedFileHandles(p)
	if err != nil || got != 3552 {
		t.Fatalf("got %v, %v; want 3552", got, err)
	}
}

func TestFileNrThatIsEmptyOrNotANumberIsAnError(t *testing.T) {
	for _, content := range []string{"", "abc 0 1"} {
		if _, err := allocatedFileHandles(writeTemp(t, "file-nr", content)); err == nil {
			t.Errorf("%q: want an error", content)
		}
	}
	if _, err := allocatedFileHandles(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing file must be an error")
	}
}

func TestPasswdChecksumChangesWhenTheFileChanges(t *testing.T) {
	const content = "root:x:0:0:root:/root:/bin/bash\n"
	p := writeTemp(t, "passwd", content)
	first, _, err := passwdState(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := float64(crc32.ChecksumIEEE([]byte(content))); first != want {
		t.Errorf("checksum %v, want the CRC32 of the content %v", first, want)
	}
	again, _, _ := passwdState(p)
	if again != first {
		t.Error("an unchanged file must keep its checksum")
	}
	if err := os.WriteFile(p, []byte(content+"mallory:x:0:0::/:/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _, _ := passwdState(p)
	if changed == first {
		t.Error("a changed file must change the checksum")
	}
}

func TestPasswdModifiedTimeIsTheFileModTime(t *testing.T) {
	p := writeTemp(t, "passwd", "root:x:0:0:root:/root:/bin/bash\n")
	at := time.Unix(1700000000, 0)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	_, modified, err := passwdState(p)
	if err != nil || modified != 1700000000 {
		t.Fatalf("got %v, %v; want 1700000000", modified, err)
	}
}

func TestPasswdMissingIsAnError(t *testing.T) {
	if _, _, err := passwdState(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("want an error")
	}
}
