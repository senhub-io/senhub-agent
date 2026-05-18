//go:build prod_smoke

package prod_smoke

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// host bundles the SSH coordinates of a target. Resolved lazily from
// the senhub secret store so the test process never holds the password
// in a long-lived variable.
type host struct {
	Name     string // logical alias used in test output ("sha901", "bbcloud")
	SecretNS string // secret store namespace, e.g. "ssh.sha901"
	Default  hostDefaults
}

// hostDefaults are the bits of host config we keep in code: the SSH
// port, user, and key file (Linux uses a key; Windows targets
// fall back to password from the secret store). Used as a fallback
// when the secret store doesn't expose them as separate keys.
type hostDefaults struct {
	Port    int
	User    string
	KeyFile string // empty → password auth via sshpass
}

// hosts is the registry of target prod hosts. Adding a new target is
// one entry here plus the corresponding secrets in ~/.senhub/.
var hosts = []host{
	{
		Name:     "sha901",
		SecretNS: "ssh.sha901",
		Default:  hostDefaults{Port: 5511, User: "sfadmin", KeyFile: "~/.ssh/sfadmin_rsa"},
	},
	{
		Name:     "bbcloud",
		SecretNS: "ssh.bbcloud",
		Default:  hostDefaults{Port: 5511, User: "sfadmin"},
		// bbcloud uses password auth — KeyFile empty triggers sshpass.
	},
}

// readSecret shells out to the senhub secret-store CLI. Returns "" if
// the secret is missing — the caller decides whether to skip the test
// or fail.
func readSecret(t *testing.T, key string) string {
	t.Helper()
	cmd := exec.Command(secretReader(), key)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Logf("readSecret(%q): %v (stderr: %s)", key, err, stderr.String())
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// secretReader returns the path to read-secret.sh. Overridable via
// SENHUB_SECRET_READER env var for non-default install locations.
func secretReader() string {
	if p := envOr("SENHUB_SECRET_READER", ""); p != "" {
		return p
	}
	// Default install location per the user-level CLAUDE.md.
	if home, err := homeDir(); err == nil {
		return home + "/.senhub/read-secret.sh"
	}
	return "read-secret.sh"
}

// remoteShell runs `cmd` on the target via SSH and returns combined
// stdout+stderr. Returns ("", nil) when the host is unreachable due
// to missing credentials — caller should t.Skip in that case.
//
// The shell command is passed as the single argument to `ssh`, exactly
// as you would when typing at a terminal. Caller is responsible for
// quoting if the command contains shell metacharacters.
func remoteShell(t *testing.T, h host, cmd string) (string, bool) {
	t.Helper()

	hostAddr := readSecret(t, h.SecretNS+".host")
	if hostAddr == "" {
		t.Logf("host %s: SSH host not in secret store (key %q); skipping", h.Name, h.SecretNS+".host")
		return "", false
	}

	port := h.Default.Port
	if p := readSecret(t, h.SecretNS+".port"); p != "" {
		// Best-effort: leave port at default if parsing fails.
		var n int
		fmt.Sscanf(p, "%d", &n)
		if n > 0 {
			port = n
		}
	}
	user := h.Default.User
	if u := readSecret(t, h.SecretNS+".user"); u != "" {
		user = u
	}

	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", fmt.Sprintf("%d", port),
	}
	if h.Default.KeyFile != "" {
		args = append(args, "-i", expandHome(h.Default.KeyFile))
	}
	args = append(args, fmt.Sprintf("%s@%s", user, hostAddr), cmd)

	var sshCmd *exec.Cmd
	if h.Default.KeyFile != "" {
		sshCmd = exec.Command("ssh", args...)
	} else {
		// Password auth: read password from secret, pass to sshpass
		// via env so it never appears on a command line visible to
		// other processes.
		pw := readSecret(t, h.SecretNS+".password")
		if pw == "" {
			t.Logf("host %s: SSH password not in secret store; skipping", h.Name)
			return "", false
		}
		sshCmd = exec.Command("sshpass", append([]string{"-e", "ssh"}, args...)...)
		sshCmd.Env = append(sshCmd.Env, "SSHPASS="+pw, "PATH="+envOr("PATH", "/usr/bin:/usr/local/bin"))
	}

	var out bytes.Buffer
	sshCmd.Stdout = &out
	sshCmd.Stderr = &out
	if err := sshCmd.Run(); err != nil {
		t.Logf("host %s: ssh %q failed: %v\noutput:\n%s", h.Name, cmd, err, out.String())
		// Return what we got anyway — callers may want to inspect
		// stderr for diagnostic purposes (e.g. file-not-found is
		// expected for the no-downgrade probe).
		return out.String(), true
	}
	return out.String(), true
}
