package auto_update

import (
	"context"
	"os"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/instanceid"
	"senhub-agent.go/internal/agent/services/logger"
)

// BootGuard is the start-of-process half of the update rollback: it counts
// this boot against a pending update and confirms the update once the agent
// has stayed up. All its methods are safe on a nil receiver.
type BootGuard struct {
	env    rollbackEnv
	action bootAction
}

func (a *autoUpdate) rollbackEnvFor(binary string) rollbackEnv {
	dir := a.stateDir
	if dir == "" {
		dir = instanceid.OwnStateDir()
	}
	return newRollbackEnv(dir, binary, a.logger)
}

func newRollbackEnv(stateDir, binary string, log *logger.ModuleLogger) rollbackEnv {
	return rollbackEnv{
		fs:       osRollbackFS{},
		stateDir: stateDir,
		binary:   binary,
		version:  cliArgs.Version,
		canSwap:  selfApplyRefusalFor(runtime.GOOS, false) == "",
		now:      time.Now,
		log:      log,
	}
}

// BeginBoot runs the rollback state machine for this process. When it returns
// a guard whose RolledBack is true the previous binary has been put back and
// the caller must exit non-zero so the service manager starts it.
func BeginBoot(base *logger.Logger) *BootGuard {
	log := logger.NewModuleLogger(base, "service.auto_update")
	exe, err := os.Executable()
	if err != nil {
		log.Warn().Err(err).Msg("Cannot locate the running executable; update rollback is inactive for this boot")
		return &BootGuard{}
	}
	env := newRollbackEnv(instanceid.OwnStateDir(), exe, log)
	return &BootGuard{env: env, action: env.boot()}
}

// RolledBack reports that this boot swapped the previous binary back.
func (g *BootGuard) RolledBack() bool {
	return g != nil && g.action == bootRolledBack
}

// ConfirmWhenHealthy marks the pending update as good once the agent has run
// for the confirmation delay; it returns at once and gives up if ctx ends
// first, so a process that dies early leaves the boot counted.
func (g *BootGuard) ConfirmWhenHealthy(ctx context.Context) {
	if g == nil || g.action != bootTracking {
		return
	}
	go func() {
		timer := time.NewTimer(confirmAfter)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			if err := g.env.confirm(); err != nil {
				g.env.log.Warn().Err(err).Str("state_dir", g.env.stateDir).
					Msg("Could not confirm the update; it may be rolled back at a later restart")
				return
			}
			g.env.log.Info().Str("version", g.env.version).Msg("Update confirmed after a healthy run")
		}
	}()
}
