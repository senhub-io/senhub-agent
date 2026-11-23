//go:build linux

package auto_update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestValidRequestVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"zero", "0.0.0", true},
		{"normal", "1.2.3", true},
		{"beta", "0.7.2-beta.3", true},
		{"beta zero", "1.0.0-beta.0", true},
		{"large numbers", "9999.9999.9999", true},
		{"leading zero major", "01.2.3", false},
		{"leading zero minor", "0.02.3", false},
		{"leading zero patch", "0.0.03", false},
		{"leading zero beta", "0.7.2-beta.03", false},
		{"missing patch", "1.2", false},
		{"four parts", "1.2.3.4", false},
		{"alpha", "1.2.3-alpha.1", false},
		{"rc", "1.2.3-rc.1", false},
		{"beta no number", "1.2.3-beta", false},
		{"beta empty", "1.2.3-beta.", false},
		{"negative", "-1.2.3", false},
		{"plus build", "1.2.3+build", false},
		{"v prefix", "v1.2.3", false},
		{"spaces", " 1.2.3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidRequestVersion(tt.version); got != tt.want {
				t.Errorf("ValidRequestVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestConsumeRequest(t *testing.T) {
	uid := uint32(os.Getuid())
	log := createTestModuleLogger()

	// Helper to create a request dir and optionally a file with given content/mode/type.
	setupRequest := func(t *testing.T, dirMode os.FileMode, setup func(dir string) error) (dir string, cleanup func()) {
		t.Helper()
		dir = t.TempDir()
		if err := os.Chmod(dir, dirMode); err != nil {
			t.Fatal(err)
		}
		if setup != nil {
			if err := setup(dir); err != nil {
				t.Fatal(err)
			}
		}
		return dir, func() {}
	}

	filePath := func(dir string) string { return filepath.Join(dir, RootUpdateRequestFile) }

	t.Run("valid version", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o640)
		})
		defer cleanup()
		got, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if err != nil {
			t.Fatalf("consumeRequest returned error: %v", err)
		}
		if got != "0.7.2" {
			t.Errorf("got version %q, want %q", got, "0.7.2")
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("valid beta", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2-beta.3\n"), 0o640)
		})
		defer cleanup()
		got, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if err != nil {
			t.Fatalf("consumeRequest returned error: %v", err)
		}
		if got != "0.7.2-beta.3" {
			t.Errorf("got version %q, want %q", got, "0.7.2-beta.3")
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("empty", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte{}, 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("no trailing newline", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("two lines", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n0.8.0\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("v prefix", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("v0.7.2\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("space before newline", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2 \n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("carriage return", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\r\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("leading zero", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("01.2.3\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("rc version", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2-rc.1\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("beta without number", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2-beta\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("NUL byte", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\x00\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("too long 100 bytes", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"+strings.Repeat("a", 100)), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("exactly 65 bytes", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"+strings.Repeat("a", 59)), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("symlink to valid file refused, target kept", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, nil)
		defer cleanup()
		targetPath := filepath.Join(dir, "target")
		if err := os.WriteFile(targetPath, []byte("0.7.2\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		linkPath := filePath(dir)
		if err := os.Symlink(targetPath, linkPath); err != nil {
			t.Fatal(err)
		}
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// symlink removed
		if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
			t.Errorf("symlink should be removed, got err %v", err)
		}
		// target still exists and content intact
		data, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "0.7.2\n" {
			t.Errorf("target content changed: %q", data)
		}
	})

	t.Run("symlink to /etc/passwd refused, target untouched", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, nil)
		defer cleanup()
		linkPath := filePath(dir)
		if err := os.Symlink("/etc/passwd", linkPath); err != nil {
			t.Fatal(err)
		}
		// stat /etc/passwd before
		before, err := os.Stat("/etc/passwd")
		if err != nil {
			t.Fatal(err)
		}
		_, err = consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// symlink removed
		if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
			t.Errorf("symlink should be removed")
		}
		after, err := os.Stat("/etc/passwd")
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Errorf("/etc/passwd modified")
		}
	})

	t.Run("wrong owner", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid+1, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("group writable", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			if err := os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o600); err != nil {
				return err
			}
			return os.Chmod(filePath(dir), 0o660)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("other writable", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			if err := os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o600); err != nil {
				return err
			}
			return os.Chmod(filePath(dir), 0o602)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("mode 0640 accepted", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("mode 0600 accepted", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o600)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("hard link refused", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			if err := os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o640); err != nil {
				return err
			}
			// create second hard link
			return os.Link(filePath(dir), filepath.Join(dir, "other"))
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// symlink removed, other link still exists
		if _, err := os.Lstat(filePath(dir)); !os.IsNotExist(err) {
			t.Errorf("original file should be removed")
		}
		if _, err := os.Stat(filepath.Join(dir, "other")); err != nil {
			t.Errorf("other hard link should exist")
		}
	})

	t.Run("FIFO refused", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, func(dir string) error {
			return unix.Mkfifo(filePath(dir), 0o640)
		})
		defer cleanup()
		done := make(chan error, 1)
		go func() {
			_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
			done <- err
		}()
		select {
		case err := <-done:
			if !errors.Is(err, ErrRequestRefused) {
				t.Fatalf("expected ErrRequestRefused, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("consumeRequest hung on FIFO")
		}
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("request path is directory", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, nil)
		defer cleanup()
		// create a directory at request path
		if err := os.Mkdir(filePath(dir), 0o750); err != nil {
			t.Fatal(err)
		}
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// directory should be removed by unlinkat (fails, but we can check)
		// Actually unlinkat on directory with flags 0 will fail with EISDIR, but consumeRequest's defer will try and log warning.
		// We'll check that the directory still exists? The defer tries unlinkat(dirfd, file, 0). unlink on directory without AT_REMOVEDIR will return EISDIR.
		// The requirement: request file (the link itself for a symlink) is deleted afterwards. For directory, we might not expect deletion.
		// But the defer will attempt removal and fail. We'll not assert removal for this case.
	})

	t.Run("request dir group-writable", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o770, func(dir string) error {
			return os.WriteFile(filePath(dir), []byte("0.7.2\n"), 0o640)
		})
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// file not removed because dir open failed? Actually dir open with O_NOFOLLOW will succeed, but Fstat on dir will fail due to mode? The directory mode check is after open, so it will refuse and defer unlinkat will try to remove file. Since dirfd is valid, it should remove the file even though refusal happens after. We can check removal.
		assertFileRemoved(t, filePath(dir))
	})

	t.Run("request dir is symlink", func(t *testing.T) {
		realDir := t.TempDir()
		if err := os.Chmod(realDir, 0o750); err != nil {
			t.Fatal(err)
		}
		// create symlink dir -> realDir
		linkDir := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(realDir, linkDir); err != nil {
			t.Fatal(err)
		}
		// put request file in realDir
		if err := os.WriteFile(filepath.Join(realDir, RootUpdateRequestFile), []byte("0.7.2\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		_, err := consumeRequest(linkDir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
		// symlink not removed? The dirfd is opened on the symlink itself with O_NOFOLLOW, will fail, so unlinkat will not happen. So the request file remains in realDir.
		if _, err := os.Stat(filepath.Join(realDir, RootUpdateRequestFile)); err != nil {
			t.Errorf("real request file should still exist")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir, cleanup := setupRequest(t, 0o750, nil)
		defer cleanup()
		_, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
		if !errors.Is(err, ErrRequestRefused) {
			t.Fatalf("expected ErrRequestRefused, got %v", err)
		}
	})
}

func assertFileRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("file %s should be removed, got err %v", path, err)
	}
}

func TestRequestWritesAtomicallyAndConsumeAcceptsIt(t *testing.T) {
	uid := uint32(os.Getuid())
	log := createTestModuleLogger()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	installer := &packageInstaller{
		requestDir:  dir,
		requestFile: RootUpdateRequestFile,
		marker:      "", // not used
		pathUnit:    "",
		wantsGlob:   "",
		maskPath:    "",
	}

	// Test valid request
	if err := installer.Request("0.7.2"); err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	// check mode
	fi, err := os.Stat(filepath.Join(dir, RootUpdateRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("request file mode = %o, want 0640", fi.Mode().Perm())
	}

	// consume
	got, err := consumeRequest(dir, RootUpdateRequestFile, uid, log)
	if err != nil {
		t.Fatalf("consumeRequest error: %v", err)
	}
	if got != "0.7.2" {
		t.Errorf("got %q, want 0.7.2", got)
	}
	// no leftovers
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}

	// Test invalid requests
	invalidVersions := []string{
		"0.7.2\n",
		"../x",
		"",
		"1.2",
		"latest",
	}
	for _, v := range invalidVersions {
		if err := installer.Request(v); err == nil {
			t.Errorf("Request(%q) should fail", v)
		}
		// Ensure no file created (or if created, cleaned up)
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("leftover files after invalid request %q: %v", v, entries)
		}
	}
}

func TestPackageInstallerAvailable(t *testing.T) {
	// We'll build a temp tree and set the packageInstaller fields.
	base := t.TempDir()
	marker := filepath.Join(base, "marker")
	pathUnit := filepath.Join(base, "path-unit")
	wantsDir := filepath.Join(base, "multi-user.target.wants")
	maskPath := filepath.Join(base, "mask")
	requestDir := filepath.Join(base, "request-dir")

	// Create directories
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(requestDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create marker and path unit regular files
	if err := os.WriteFile(marker, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathUnit, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create wants symlink
	if err := os.Symlink(pathUnit, filepath.Join(wantsDir, "senhub-agent-update.path")); err != nil {
		t.Fatal(err)
	}

	installer := &packageInstaller{
		requestDir:  requestDir,
		requestFile: RootUpdateRequestFile,
		marker:      marker,
		pathUnit:    pathUnit,
		wantsGlob:   filepath.Join(base, "*.wants", "senhub-agent-update.path"),
		maskPath:    maskPath,
	}

	// maskPath absent -> Available should be true
	if !installer.Available() {
		t.Fatal("Available should be true when all prerequisites met and no mask")
	}

	// maskPath as symlink to /dev/null -> false
	if err := os.Symlink("/dev/null", maskPath); err != nil {
		t.Fatal(err)
	}
	if installer.Available() {
		t.Fatal("Available should be false when mask is symlink to /dev/null")
	}
	// remove mask symlink
	os.Remove(maskPath)

	// maskPath as regular file -> true (per code only checks symlink to /dev/null)
	if err := os.WriteFile(maskPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !installer.Available() {
		t.Fatal("Available should be true when mask path is regular file")
	}
	os.Remove(maskPath)

	// missing marker -> false
	os.Remove(marker)
	if installer.Available() {
		t.Fatal("Available should be false when marker missing")
	}
	// restore
	os.WriteFile(marker, []byte(""), 0o644)

	// missing path unit -> false
	os.Remove(pathUnit)
	if installer.Available() {
		t.Fatal("Available should be false when path unit missing")
	}
	// restore
	os.WriteFile(pathUnit, []byte(""), 0o644)

	// no wants symlink -> false
	os.Remove(filepath.Join(wantsDir, "senhub-agent-update.path"))
	if installer.Available() {
		t.Fatal("Available should be false when wants symlink missing")
	}
	// restore
	os.Symlink(pathUnit, filepath.Join(wantsDir, "senhub-agent-update.path"))

	// request dir not writable -> false (simulate by chmod 0o500)
	if err := os.Chmod(requestDir, 0o500); err != nil {
		t.Fatal(err)
	}
	if installer.Available() {
		t.Fatal("Available should be false when request dir not writable")
	}
	// restore
	os.Chmod(requestDir, 0o750)
}
