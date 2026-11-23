//go:build linux

package auto_update

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// writeRunResult reports a run to the service. Failing to is only logged: the
// run itself is already done.
func writeRunResult(opts RootApplyOptions, st *applyState, runErr error) {
	res := UpdateResult{Version: st.version, Outcome: ResultInstalled, At: opts.Now().UTC()}
	if runErr != nil {
		res.Outcome, res.Reason = ResultRefused, "install"
		var rerr *RootApplyError
		if errors.As(runErr, &rerr) {
			res.Reason = reasonForStep(rerr.Step, rerr.Err)
			if rerr.Step == "restart" {
				res.Outcome = ResultRolledBack
			}
		}
	} else if st.rollback {
		res.Outcome = ResultRolledBack
	}
	if err := writeResultFile(opts.StateDir, res); err != nil {
		opts.Logger.Warn().Err(err).Msg("Could not write the update result for the service")
	}
}

// reasonForStep names a refusal with the codes a server already knows.
func reasonForStep(step string, err error) string {
	switch step {
	case "rate":
		return ReasonRateLimited
	case "list":
		return ReasonSignature
	case "published":
		return CodeTargetNotPublished
	case "downgrade":
		return CodeDowngradeNotAllowed
	case "edition":
		return ReasonNotReplace
	case "install":
		return failureReason(err)
	case "lock":
		return "busy"
	case "request", "rollback", "restart":
		return step
	}
	return ReasonInstall
}

// writeResultFile replaces update-result.json in dir. The directory belongs to
// the service user, so nothing goes through a path: the directory is opened
// without following a link, the file is created exclusively next to its
// destination, its mode is set on the open file, and it is renamed into place,
// which replaces a link the service planted instead of following it.
func writeResultFile(dir string, res UpdateResult) error {
	data, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("encoding the result: %w", err)
	}
	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("opening %s: %w", dir, err)
	}
	defer func() { _ = unix.Close(dirfd) }()
	rnd := make([]byte, 8)
	if _, err := rand.Read(rnd); err != nil {
		return fmt.Errorf("naming the result file: %w", err)
	}
	tmp := ".update-result-" + hex.EncodeToString(rnd)
	fd, err := unix.Openat(dirfd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("creating the result file: %w", err)
	}
	fail := func(err error) error {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(dirfd, tmp, 0)
		return err
	}
	if _, err := unix.Write(fd, data); err != nil {
		return fail(fmt.Errorf("writing the result file: %w", err))
	}
	if err := unix.Fchmod(fd, 0o644); err != nil {
		return fail(fmt.Errorf("setting the mode of the result file: %w", err))
	}
	if err := unix.Fsync(fd); err != nil {
		return fail(fmt.Errorf("flushing the result file: %w", err))
	}
	if err := unix.Close(fd); err != nil {
		_ = unix.Unlinkat(dirfd, tmp, 0)
		return fmt.Errorf("closing the result file: %w", err)
	}
	if err := unix.Renameat(dirfd, tmp, dirfd, updateResultFile); err != nil {
		_ = unix.Unlinkat(dirfd, tmp, 0)
		return fmt.Errorf("publishing the result file: %w", err)
	}
	return nil
}
