//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

const serviceStateDir = "/var/lib/senhub-agent"

// ensureServiceUser creates the dedicated system user/group the
// hardened unit runs as, mirroring what the .deb/.rpm postinstall
// script does (packaging/scripts/postinstall.sh) so a ZIP/CLI install
// reaches the same posture as a package install. Idempotent: an
// existing user is left untouched.
func ensureServiceUser(name string) error {
	if name == rootServiceUser {
		return nil
	}
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	if _, err := user.LookupGroup(name); err != nil {
		if err := runFirstAvailable([][]string{
			{"groupadd", "--system", name},
			{"addgroup", "--system", name},
		}); err != nil {
			return fmt.Errorf("creating system group %q: %w", name, err)
		}
	}
	if err := runFirstAvailable([][]string{
		{"useradd", "--system", "--gid", name,
			"--home-dir", serviceStateDir, "--no-create-home",
			"--shell", "/usr/sbin/nologin", name},
		{"adduser", "--system", "--ingroup", name,
			"--home", serviceStateDir, "--no-create-home",
			"--shell", "/usr/sbin/nologin", name},
	}); err != nil {
		return fmt.Errorf("creating system user %q: %w", name, err)
	}
	return nil
}

// runFirstAvailable runs the first candidate command whose binary is
// on PATH (useradd on most distros, adduser on busybox/alpine).
func runFirstAvailable(candidates [][]string) error {
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w (%s)", c[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return fmt.Errorf("no user management tool found (tried %s)", candidates[0][0])
}

// chownServiceTree hands the install-time artifacts (config directory,
// log directory, TLS certs) to the service user. The installer runs as
// root, so without this step the unprivileged daemon could not read
// the 0600 config files the install just generated.
func chownServiceTree(name, configPath string) error {
	if name == rootServiceUser {
		return nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("looking up service user %q: %w", name, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parsing uid %q for user %q: %w", u.Uid, name, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parsing gid %q for user %q: %w", u.Gid, name, err)
	}

	roots := []string{filepath.Dir(configPath), agentLogger.LogBaseDir()}
	if wd, wdErr := os.Getwd(); wdErr == nil {
		roots = append(roots, filepath.Join(wd, "certs"))
	}
	for _, root := range roots {
		if _, statErr := os.Stat(root); statErr != nil {
			continue
		}
		walkErr := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(p, uid, gid)
		})
		if walkErr != nil {
			return fmt.Errorf("changing ownership of %s to %s: %w", root, name, walkErr)
		}
	}
	return nil
}
