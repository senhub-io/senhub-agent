//go:build windows

package execprobe

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
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

// procTree holds the Job Object a run's child is assigned to. The job
// is created with KILL_ON_JOB_CLOSE, so the whole tree dies when the
// handle closes, including when the agent itself dies.
type procTree struct {
	mu  sync.Mutex
	job windows.Handle
}

// attach puts the started child in a new job. On failure the tree
// falls back to taskkill /T.
func (t *procTree) attach(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("creating job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("configuring job object: %w", err)
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("opening child process: %w", err)
	}
	defer windows.CloseHandle(proc) //nolint:errcheck // best-effort release
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("assigning child to job object: %w", err)
	}
	t.mu.Lock()
	t.job = job
	t.mu.Unlock()
	return nil
}

// kill ends the whole tree: the job when one is attached, taskkill /T
// otherwise.
func (t *procTree) kill(cmd *exec.Cmd) error {
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != 0 {
		return windows.TerminateJobObject(job, 1)
	}
	return killProcessGroup(cmd)
}

// close releases the job handle, which also ends any process still in it.
func (t *procTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
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
