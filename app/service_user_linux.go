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

	"senhub-agent.go/internal/agent/cliArgs"
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
// Tries useradd (util-linux/shadow) then Debian adduser; busybox
// adduser needs different flags (-S/-G/-H) and is not covered —
// on such systems pre-create the user or install with --user root.
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

// installArtifactPaths decides exactly what the installer hands to the
// service user. The first version recursively chowned
// filepath.Dir(configPath) and $CWD/certs — with
// --config-path /etc/agent.yaml that is "chown -R senhub /etc", an
// escalation primitive inside a least-privilege change (#280 review
// finding). Now: directories are walked ONLY when they are the
// agent's own canonical directories (default config dir, log dir, the
// certs dir generated next to the config when HTTPS is enabled); a
// custom config path gets its FILE chowned, nothing else.
func installArtifactPaths(configPath, defaultConfigDir, logDir string, enableHTTPS bool) (recursive []string, files []string) {
	cfgDir := filepath.Dir(configPath)
	if cfgDir == defaultConfigDir {
		recursive = append(recursive, cfgDir)
	} else {
		files = append(files, configPath)
		if enableHTTPS {
			// generateTLSCertificates writes next to the config.
			recursive = append(recursive, filepath.Join(cfgDir, "certs"))
		}
	}
	recursive = append(recursive, logDir)
	return recursive, files
}

// chownServiceTree hands the install-generated artifacts (config, log
// directory, TLS certs) to the service user. The installer runs as
// root, so without this step the unprivileged daemon could not read
// the 0600 config files the install just generated. Symlinks are
// changed, never followed (Lchown): a symlink inside a walked
// directory must not re-own its target elsewhere on the filesystem.
func chownServiceTree(name, configPath string, enableHTTPS bool) error {
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

	defaultPath, err := cliArgs.GetAbsoluteConfigPath("")
	if err != nil {
		return fmt.Errorf("resolving the default config path: %w", err)
	}
	recursive, files := installArtifactPaths(configPath, filepath.Dir(defaultPath), agentLogger.LogBaseDir(), enableHTTPS)

	for _, f := range files {
		if _, statErr := os.Stat(f); statErr != nil {
			continue
		}
		if chErr := os.Lchown(f, uid, gid); chErr != nil {
			return fmt.Errorf("changing ownership of %s to %s: %w", f, name, chErr)
		}
	}
	for _, root := range recursive {
		if _, statErr := os.Stat(root); statErr != nil {
			continue
		}
		walkErr := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(p, uid, gid)
		})
		if walkErr != nil {
			return fmt.Errorf("changing ownership of %s to %s: %w", root, name, walkErr)
		}
	}
	return nil
}
