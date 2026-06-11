//go:build !windows

package execprobe

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// configureSysProcAttr puts the child in its own process group so a
// timeout kill reaches the whole tree, not just the direct child
// (same handling as the linux_logs journalctl subprocess).
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup terminates the child's whole process group.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// checkExecutable refuses targets anyone on the host could rewrite:
// the probe runs as the agent user (root on Linux), so a
// world-writable script is a privilege escalation handed to every
// local user.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("exec command %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("exec command %s is a directory", path)
	}
	if info.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("exec command %s is world-writable (%o); refusing to run it as the agent user", path, info.Mode().Perm())
	}
	return nil
}
