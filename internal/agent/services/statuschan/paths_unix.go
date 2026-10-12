//go:build !windows

package statuschan

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const defaultLinuxStateDir = "/var/lib/senhub-agent"

// candidatePaths lists where the socket may be, most specific first: the
// service's StateDirectory (set by systemd for the daemon), the packaged
// default for the CLI that does not inherit it, then the configuration
// directory for hosts with neither (containers, a macOS development
// run).
func candidatePaths(configPath string) []string {
	var dirs []string
	if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
		dirs = append(dirs, strings.Split(sd, ":")[0])
	}
	if runtime.GOOS == "linux" {
		dirs = append(dirs, defaultLinuxStateDir)
	}
	if configPath != "" {
		dirs = append(dirs, filepath.Dir(configPath))
	}
	dirs = append(dirs, fallbackDir())
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, socketIn(d))
	}
	return out
}

// fallbackDir holds the socket when no other directory can: a state and
// configuration directory both on an SMB share (Azure Files), which has
// no Unix sockets. Created private on first use.
func fallbackDir() string {
	return filepath.Join(os.TempDir(), "senhub-agent")
}

// ensurePrivateDir creates dir 0700, or accepts it only when it is a real
// directory owned by this process and closed to others, so a directory
// another user prepared in the shared temp directory is never used.
func ensurePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || !ownedByMe(fi) {
		return fmt.Errorf("%s is not a private directory of this user", dir)
	}
	return nil
}

func listen(paths []string) (net.Listener, string, error) {
	var lastErr error
	for _, p := range paths {
		if filepath.Dir(p) == fallbackDir() {
			if err := ensurePrivateDir(filepath.Dir(p)); err != nil {
				lastErr = err
				continue
			}
		}
		if !dirExists(filepath.Dir(p)) {
			continue
		}
		ln, err := listenAt(p)
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no state directory to hold the socket")
	}
	return nil, "", lastErr
}

func listenAt(path string) (net.Listener, error) {
	// Only a leftover socket of ours is removed; anything else at that
	// path is not ours to delete.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return nil, fmt.Errorf("%s is already served by another process", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing stale socket %s: %w", path, err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", path, err)
	}
	// Connecting to a Unix socket needs write permission on the file:
	// owner only, the service account and root.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("restricting %s: %w", path, err)
	}
	return ln, nil
}

func dial(path string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", path, timeout)
}

func cleanup(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}
