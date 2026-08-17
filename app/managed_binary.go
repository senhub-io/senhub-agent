package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"strconv"
)

// systemBinaryDir is where `agent install` puts the one agent binary on Linux.
// It is root-owned and sits inside ProtectSystem=full's read-only tree, so the
// daemon cannot write it.
//
// That is the entire point, and it is a security property rather than a tidiness
// one. The daemon runs unprivileged and parses untrusted input — OTLP off the
// network, SNMP traps, syslog, log files, responses from probed targets — so it
// is the component most likely to be compromised. An agent that can replace its
// own binary hands whoever compromises it persistence across restarts, which on
// a monitoring agent that runs everywhere and restarts itself is the prize worth
// having. And a signature check performed by the process that may itself be
// compromised is not a check: the attacker owns the code doing the checking.
//
// Installing is therefore a root action, done by something the daemon cannot
// influence: `sudo senhub-agent install` today, apt/dnf/zypper tomorrow.
//
// /usr/local/bin is the FHS location for locally installed software. A distro
// package must not write there (Debian Policy reserves /usr/local for the local
// administrator), so packaged installs land in /usr/bin instead and the unit
// they ship points there; both are root-owned and equally unwritable by the
// daemon, which is what matters here.
const systemBinaryDir = "/usr/local/bin"

// systemBinaryName is the installed file name, shared by both layouts.
const systemBinaryName = "senhub-agent"

// legacyManagedBinaryDir is where installs before 0.5.4 staged a SECOND copy of
// the binary: a service-user-owned directory under the StateDirectory, writable
// by the daemon and outside ProtectSystem=full, so in-process auto-update could
// rename a new binary over the running one (#571).
//
// It worked, and it was the wrong shape. It left the agent on disk twice, let
// the two drift apart (#723), and gave the unprivileged daemon write access to
// an executable that systemd runs. It is retained here only so install and
// refresh-unit can recognise a host that still carries it and clean it up (#794).
const legacyManagedBinaryDir = "/var/lib/senhub-agent/bin"

// legacyManagedBinaryPath is the old second copy, as an absolute path.
func legacyManagedBinaryPath() string {
	return legacyManagedBinaryDir + "/" + systemBinaryName
}

// installSystemBinary places the running executable at its system location and
// returns that path. Idempotent: installing from the system path itself is a
// no-op copy.
//
// The result is root-owned 0755 and is deliberately NOT chowned to the service
// user. An earlier version did exactly that so the daemon could self-update;
// removing it is the change.
func installSystemBinary(srcExe string) (string, error) {
	dst := filepath.Join(systemBinaryDir, systemBinaryName)
	if err := os.MkdirAll(systemBinaryDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", systemBinaryDir, err)
	}
	if srcExe != dst {
		if err := copyExecutable(srcExe, dst); err != nil {
			return "", fmt.Errorf("installing binary to %s: %w", dst, err)
		}
	}
	// Explicitly root-owned. On a host upgrading from the old layout the file
	// may already exist owned by the service user, and inheriting that would
	// silently keep the daemon's write access — the one thing this is for.
	if err := os.Chown(dst, 0, 0); err != nil {
		return "", fmt.Errorf("setting root ownership on %s: %w", dst, err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return "", fmt.Errorf("setting mode on %s: %w", dst, err)
	}
	return dst, nil
}

// removeLegacyManagedBinary deletes the pre-0.5.4 second copy and its directory.
//
// Failures are returned but are not fatal to an install: the host is already in
// the correct shape once the unit points at the system binary, and a leftover
// file is untidy rather than dangerous — it is no longer executed by anything.
// The caller reports it instead of aborting a working install over cleanup.
func removeLegacyManagedBinary() error {
	if _, err := os.Stat(legacyManagedBinaryPath()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.RemoveAll(legacyManagedBinaryDir); err != nil {
		return fmt.Errorf("removing the legacy binary directory %s: %w", legacyManagedBinaryDir, err)
	}
	return nil
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
	// installed binary MUST be executable for systemd to run it.
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// chownToUser sets path's owner+group to the named user. Used for the state and
// log directories the daemon legitimately writes; never for an executable.
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
