//go:build windows

package execprobe

import (
	"fmt"
	"os"
	"os/exec"
)

// configureSysProcAttr is a no-op on Windows: exec.CommandContext's
// default kill terminates the direct child, and Windows has no Unix
// process groups to detach into.
func configureSysProcAttr(cmd *exec.Cmd) {}

// killProcessGroup falls back to killing the direct child.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// checkExecutable validates the target exists and is a file. The
// Unix world-writable check has no portable ACL equivalent here;
// the doc page covers locking down script ACLs on Windows.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("exec command %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("exec command %s is a directory", path)
	}
	return nil
}
