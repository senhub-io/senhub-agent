package app

import (
	"fmt"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"strconv"
)

// managedBinaryDir is where `agent install` stages the daemon binary on a
// hardened (non-root) Linux install. It sits under the service's
// StateDirectory (/var/lib/senhub-agent), which systemd creates owned by the
// service user and lists in ReadWritePaths — so it is BOTH writable by the
// unprivileged daemon AND outside ProtectSystem=full's read-only tree. That is
// what lets auto-update replace the binary in place: the daemon (User=senhub)
// writes <dir>/.senhub-agent.new and atomically renames it over the running
// binary. A binary left in /usr/bin or /usr/local/bin cannot be replaced — it
// is read-only (ProtectSystem) and root-owned (#571).
const managedBinaryDir = "/var/lib/senhub-agent/bin"

// installManagedBinary copies the running executable into the service-user-owned
// managedBinaryDir and returns the managed path. Idempotent: re-running install
// from the managed path is a no-op copy.
func installManagedBinary(srcExe, serviceUser string) (string, error) {
	dst := filepath.Join(managedBinaryDir, "senhub-agent")
	if err := os.MkdirAll(managedBinaryDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", managedBinaryDir, err)
	}
	if srcExe != dst {
		if err := copyExecutable(srcExe, dst); err != nil {
			return "", fmt.Errorf("staging binary to %s: %w", dst, err)
		}
	}
	// The daemon runs as serviceUser and must own the directory + binary to
	// replace the binary in place during auto-update.
	if err := chownToUser(managedBinaryDir, serviceUser); err != nil {
		return "", err
	}
	if err := chownToUser(dst, serviceUser); err != nil {
		return "", err
	}
	return dst, nil
}

// copyExecutable copies src to dst via a temp file + atomic rename (0755).
func copyExecutable(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 - src is os.Executable()
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) // #nosec G304
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// OpenFile's mode is masked by umask, which can strip the exec bits (the CI
	// runner did). chmod is umask-independent, so set 0755 explicitly — the
	// staged binary MUST be executable for systemd to run it.
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// chownToUser sets path's owner+group to the named user. A no-op (nil) when the
// user can't be resolved is wrong here — auto-update silently breaks — so a
// lookup failure is a hard error the installer surfaces.
func chownToUser(path, username string) error {
	u, err := osuser.Lookup(username)
	if err != nil {
		return fmt.Errorf("looking up service user %q: %w", username, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parsing uid for %q: %w", username, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parsing gid for %q: %w", username, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s to %s: %w", path, username, err)
	}
	return nil
}
