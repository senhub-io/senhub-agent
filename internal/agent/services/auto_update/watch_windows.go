//go:build windows

package auto_update

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"senhub-agent.go/internal/agent/cliArgs"
)

// startFailedStartWatcher spawns a detached copy of the previous binary that
// watches whether the service ever comes up on the new one. The watcher must
// outlive the service process because the service exits right after launching
// it.
func (a *autoUpdate) startFailedStartWatcher(toVersion string) {
	warn := func(err error) {
		a.logger.Warn().
			Err(err).
			Msg("Could not start the update watcher; a build that fails to start will not be rolled back")
	}
	if toVersion == "" {
		warn(errors.New("no resolved target version"))
		return
	}
	target, err := a.targetBinaryPath()
	if err != nil {
		warn(err)
		return
	}
	env := a.rollbackEnvFor(target)
	previous := env.previousPath()
	if _, err := os.Stat(previous); err != nil {
		warn(err)
		return
	}
	watchExe := filepath.Join(env.stateDir, "update-watch.exe")
	if err := copyFile(previous, watchExe); err != nil {
		warn(err)
		return
	}
	cmd := exec.Command(watchExe, "update-watch",
		"--binary", target,
		"--version", toVersion,
		"--from", cliArgs.Version,
		"--state-dir", env.stateDir,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		warn(err)
		return
	}
	if err := cmd.Process.Release(); err != nil {
		a.logger.Debug().Err(err).Msg("Releasing the update watcher handle failed")
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only handle
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
