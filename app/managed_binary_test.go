//go:build linux

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("BINARY-CONTENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "managed", "senhub-agent")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(src, dst); err != nil {
		t.Fatalf("copyExecutable: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "BINARY-CONTENT" {
		t.Fatalf("dst content = %q (err %v), want BINARY-CONTENT", b, err)
	}
	fi, _ := os.Stat(dst)
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("dst not executable: %v", fi.Mode())
	}
	if _, err := os.Stat(dst + ".new"); !os.IsNotExist(err) {
		t.Error("temp .new file should not remain after atomic rename")
	}
}

func TestChownToUser_UnknownUserErrors(t *testing.T) {
	// auto-update silently breaks if ownership is wrong, so a failed lookup
	// must be a hard error, never a silent no-op (#571).
	if err := chownToUser(t.TempDir(), "no-such-user-7f3a"); err == nil {
		t.Error("expected an error for an unknown service user")
	}
}

func TestSameContents(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	running := write("running", "0.6.0-beta")
	stale := write("stale", "0.5.5-dev!")
	copyOf := write("copy", "0.6.0-beta")

	cases := []struct {
		name string
		b    string
		want bool
	}{
		{"a stale binary of the same size is different", stale, false},
		{"an identical copy is the same", copyOf, true},
		{"a missing target is different", filepath.Join(dir, "absent"), false},
	}
	for _, c := range cases {
		got, err := sameContents(running, c.b)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: sameContents = %v, want %v", c.name, got, c.want)
		}
	}
}
