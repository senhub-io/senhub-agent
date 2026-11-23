//go:build linux

package auto_update

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func requestDirFixture(t *testing.T) (dir string, uid uint32) {
	t.Helper()
	dir = t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return dir, uint32(os.Getuid())
}

func entryGone(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Errorf("%s is still there: %v", name, err)
	}
}

func TestRemoveRequestsTakesAnyEntryTheServiceCanMake(t *testing.T) {
	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "victim")
	if err := os.WriteFile(victim, []byte("KEEP"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(t *testing.T, dir string){
		"empty directory": func(t *testing.T, dir string) {
			mustMkdir(t, filepath.Join(dir, "request"))
		},
		"directory with a file": func(t *testing.T, dir string) {
			mustMkdir(t, filepath.Join(dir, "request"))
			mustWrite(t, filepath.Join(dir, "request", "x"), "x")
		},
		"nested directories with a link to a victim directory and file": func(t *testing.T, dir string) {
			mustMkdir(t, filepath.Join(dir, "request", "a", "b", "c"))
			mustWrite(t, filepath.Join(dir, "request", "a", "b", "c", "f"), "x")
			if err := os.Symlink(victimDir, filepath.Join(dir, "request", "a", "link-dir")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, filepath.Join(dir, "request", "a", "b", "link-file")); err != nil {
				t.Fatal(err)
			}
		},
		"directory without permissions": func(t *testing.T, dir string) {
			if os.Geteuid() == 0 {
				t.Skip("root ignores the mode")
			}
			mustMkdir(t, filepath.Join(dir, "request", "closed"))
			mustWrite(t, filepath.Join(dir, "request", "closed", "f"), "x")
			if err := os.Chmod(filepath.Join(dir, "request", "closed"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T, dir string) {
			if err := unix.Mkfifo(filepath.Join(dir, "request"), 0o640); err != nil {
				t.Fatal(err)
			}
		},
		"unix socket": func(t *testing.T, dir string) {
			if err := unix.Mknod(filepath.Join(dir, "request"), unix.S_IFSOCK|0o600, 0); err != nil {
				t.Skip("cannot create a socket node:", err)
			}
		},
		"symlink to a directory": func(t *testing.T, dir string) {
			if err := os.Symlink(victimDir, filepath.Join(dir, "request")); err != nil {
				t.Fatal(err)
			}
		},
		"regular file": func(t *testing.T, dir string) { mustWrite(t, filepath.Join(dir, "request"), "0.7.2\n") },
	}
	for name, make := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := requestDirFixture(t)
			make(t, dir)
			mustMkdir(t, filepath.Join(dir, "rollback"))
			left, err := removeRequests(dir, []string{"request", "rollback"})
			if len(left) != 0 || err != nil {
				t.Fatalf("left %v, %v", left, err)
			}
			entryGone(t, dir, "request")
			entryGone(t, dir, "rollback")
			if b, err := os.ReadFile(victim); err != nil || string(b) != "KEEP" {
				t.Fatal("something outside the request directory was touched")
			}
			if _, err := os.Stat(victimDir); err != nil {
				t.Fatal("the target of a link was removed")
			}
		})
	}
}

func TestRemoveRequestsDoesNotFollowTheDirectoryItself(t *testing.T) {
	real := t.TempDir()
	mustWrite(t, filepath.Join(real, "request"), "x")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	left, err := removeRequests(link, []string{"request"})
	if err == nil || len(left) == 0 {
		t.Fatalf("a request directory reached through a link was cleaned: %v %v", left, err)
	}
	if _, err := os.Stat(filepath.Join(real, "request")); err != nil {
		t.Error("the file behind the link was removed")
	}
}

func TestRemoveRequestsReportsWhatItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes what a mode protects")
	}
	dir, _ := requestDirFixture(t)
	mustMkdir(t, filepath.Join(dir, "request", "locked"))
	mustWrite(t, filepath.Join(dir, "request", "locked", "f"), "x")
	if err := os.Chmod(filepath.Join(dir, "request", "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "request", "locked"), 0o700) })
	left, err := removeRequests(dir, []string{"request", "rollback"})
	if len(left) != 1 || left[0] != "request" || err == nil {
		t.Fatalf("left %v, err %v", left, err)
	}
}

func TestRootApplyReportsAnEntryItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes what a mode protects")
	}
	f := newRootApplyFixture(t, "0.7.2\n")
	req := filepath.Join(f.reqDir, RootUpdateRequestFile)
	if err := os.Remove(req); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Join(req, "locked"))
	mustWrite(t, filepath.Join(req, "locked", "f"), "x")
	if err := os.Chmod(filepath.Join(req, "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(req, "locked"), 0o700) })
	if step := stepOf(t, RootApply(f.opts)); step != "request" {
		t.Fatalf("step %q", step)
	}
	if r, _ := ReadUpdateResult(f.stateDir); r == nil || r.Reason != ReasonRequestNotRemovable {
		t.Errorf("result = %+v", r)
	}
	if string(f.readBinary(t)) != "old binary" {
		t.Error("something was installed")
	}
}

func TestRootApplyConsumesADirectoryRequest(t *testing.T) {
	f := newRootApplyFixture(t, "0.7.2\n")
	req := filepath.Join(f.reqDir, RootUpdateRequestFile)
	if err := os.Remove(req); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Join(req, "a", "b"))
	mustWrite(t, filepath.Join(req, "a", "b", "f"), "x")
	if err := os.Symlink("/etc", filepath.Join(req, "a", "etc")); err != nil {
		t.Fatal(err)
	}
	if step := stepOf(t, RootApply(f.opts)); step != "request" {
		t.Fatalf("step %q", step)
	}
	entryGone(t, f.reqDir, RootUpdateRequestFile)
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Fatal("/etc was touched")
	}
	if r, _ := ReadUpdateResult(f.stateDir); r == nil || r.Reason != "request" {
		t.Errorf("result = %+v", r)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
