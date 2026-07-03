//go:build linux

package linuxlogs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// journalReader is the live wrapper around a `journalctl` subprocess
// on Linux. The subprocess runs with --follow so it emits records
// continuously; we drain its stdout line-by-line.
//
// Lifecycle:
//   - newJournalReader: spawn the subprocess + drain goroutine
//   - stop: send SIGTERM, wait briefly, escalate to SIGKILL on
//     deadline. Drain goroutine exits when stdout closes.
type journalReader struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	probeName string
	log       *logger.ModuleLogger
	tracker   *exitTracker

	wg     sync.WaitGroup
	stopMu sync.Mutex
	done   bool
}

// newJournalReader spawns `journalctl` with the configured flags and
// starts a goroutine draining its stdout. Returns immediately — the
// subprocess is asynchronous; failures arrive via the drain
// goroutine's logging.
func newJournalReader(cfg LinuxLogsProbeConfig, log *logger.ModuleLogger, probeName string) (*journalReader, error) {
	args := buildJournalctlArgs(cfg)
	cmd := exec.Command("journalctl", args...)
	// Detach the subprocess from our process group so its stdin is
	// completely closed and signals reach only it (not back to us
	// when we sent SIGTERM via the group).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// stderr piped to a logger so transient warnings ("journalctl: --
	// no entries --" etc.) are visible during debug.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start journalctl: %w", err)
	}

	r := &journalReader{
		cmd:       cmd,
		stdout:    stdout,
		probeName: probeName,
		log:       log,
		tracker:   newExitTracker(),
	}

	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		drainReader(bufio.NewReader(stdout), log, probeName)
	}()
	go func() {
		defer r.wg.Done()
		drainStderr(bufio.NewReader(stderr), log)
	}()

	// Monitor goroutine: the sole owner of cmd.Wait(). It waits for the
	// drain goroutines first — StdoutPipe/StderrPipe forbid Wait before
	// all reads complete, and the pipes reach EOF exactly when the
	// process exits — then reaps the subprocess and classifies the exit.
	// A death that stop() did not request is flagged unexpected so
	// Collect() can report the probe unhealthy; before this the drain
	// goroutines just returned silently and journalctl death was
	// invisible.
	go func() {
		r.wg.Wait()
		err := r.cmd.Wait()
		r.tracker.recordExit(err)
		if r.tracker.exitedUnexpectedly() {
			log.Error().Err(err).Msg("journalctl subprocess died unexpectedly; linux_logs probe is no longer collecting")
		}
	}()

	log.Info().
		Strs("argv", args).
		Int("pid", cmd.Process.Pid).
		Msg("journalctl subprocess started")
	return r, nil
}

// drainStderr reads journalctl warnings/errors and surfaces them as
// debug-level log lines. We don't elevate them — journalctl can be
// chatty during boot ("Journal file ... not found") and in normal
// rotation, and those messages are not actionable for an operator.
func drainStderr(r *bufio.Reader, log *logger.ModuleLogger) {
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			log.Debug().Str("source", "journalctl_stderr").Msg(line)
		}
		if err != nil {
			return
		}
	}
}

// stop terminates the journalctl subprocess and waits for the drain
// goroutines to exit. SIGTERM first; if the process hasn't exited
// before the caller's deadline (or 5 s by default) we escalate to
// SIGKILL.
func (r *journalReader) stop(ctx context.Context) error {
	r.stopMu.Lock()
	if r.done {
		r.stopMu.Unlock()
		return nil
	}
	r.done = true
	r.stopMu.Unlock()

	if r.cmd == nil || r.cmd.Process == nil {
		return nil
	}

	// Declare the exit expected before signalling, so the monitor
	// goroutine classifies the resulting death as a requested stop and
	// not an unexpected one.
	r.tracker.markStopping()

	// SIGTERM is the polite shutdown for journalctl --follow.
	_ = r.cmd.Process.Signal(syscall.SIGTERM)

	// Compute a deadline: caller's context wins; otherwise 5s default.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}

	select {
	case <-r.tracker.waitCh():
		// journalctl returns non-zero on signal — that's expected, not
		// a probe failure.
		if err := r.tracker.err(); err != nil {
			r.log.Debug().Err(err).Msg("journalctl exited with non-zero status (expected on signal)")
		}
		return nil
	case <-time.After(time.Until(deadline)):
		_ = r.cmd.Process.Kill()
		// SIGKILL closes the pipes, the drains return and the monitor
		// reaps the process; wait for that so we don't leak it.
		<-r.tracker.waitCh()
		return fmt.Errorf("journalctl did not exit before deadline; killed")
	}
}

// healthErr reports the reader unhealthy when the journalctl subprocess
// died without a stop() request. Collect() surfaces this so an operator
// sees the probe go unhealthy instead of it silently shipping nothing.
func (r *journalReader) healthErr() error {
	if !r.tracker.exitedUnexpectedly() {
		return nil
	}
	if err := r.tracker.err(); err != nil {
		return fmt.Errorf("journalctl subprocess exited unexpectedly: %w", err)
	}
	return fmt.Errorf("journalctl subprocess exited unexpectedly")
}
