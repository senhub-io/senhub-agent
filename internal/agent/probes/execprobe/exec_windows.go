//go:build windows

package execprobe

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// configureSysProcAttr is a no-op on Windows: there is no Unix
// process group to detach into; tree termination happens in
// killProcessGroup.
func configureSysProcAttr(cmd *exec.Cmd) {}

// killProcessGroup terminates the child and its descendants. Killing
// only the direct child (cmd.exe for a .bat check) leaves grandchildren
// holding the stdout/stderr pipes, which blocks Run() until they exit;
// taskkill /T walks the tree by PID. The probe's WaitDelay is the
// backstop if even that leaves a pipe holder behind.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// PID-targeted by design — never an image-name kill.
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
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
