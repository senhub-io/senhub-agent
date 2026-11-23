//go:build linux

package auto_update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ybbus/httpretry"
	"golang.org/x/sys/unix"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

const (
	rootUpdateBinary   = "/usr/bin/senhub-agent"
	rootUpdateStateDir = "/var/lib/senhub-agent"
	rootUpdateRootDir  = "/var/lib/senhub-agent-update"
)

type RootApplyOptions struct {
	Logger        *logger.ModuleLogger
	RequestDir    string
	RequestFile   string
	ServiceUID    func() (uint32, error)
	Registry      string
	HTTPClient    *http.Client
	Binary        string
	StateDir      string
	RootStateDir  string
	Restart       func(startIfStopped bool) (string, error)
	DownloadWait  *time.Duration
	ListRetryWait *time.Duration
}

type RootApplyError struct {
	Step string
	Err  error
}

func (e *RootApplyError) Error() string {
	return fmt.Sprintf("root update refused at %s: %v", e.Step, e.Err)
}

func (e *RootApplyError) Unwrap() error {
	return e.Err
}

func RootApply(opts RootApplyOptions) error {
	// Defaults
	if opts.Logger == nil {
		opts.Logger = logger.NewModuleLogger(nil, "service.root_update")
	}
	if opts.RequestDir == "" {
		opts.RequestDir = RootUpdateRequestDir
	}
	if opts.RequestFile == "" {
		opts.RequestFile = RootUpdateRequestFile
	}
	if opts.Binary == "" {
		opts.Binary = rootUpdateBinary
	}
	if opts.StateDir == "" {
		opts.StateDir = rootUpdateStateDir
	}
	if opts.RootStateDir == "" {
		opts.RootStateDir = rootUpdateRootDir
	}
	if opts.ServiceUID == nil {
		opts.ServiceUID = lookupServiceUID
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = httpretry.NewDefaultClient(httpretry.WithMaxRetryCount(3))
	}
	log := opts.Logger

	// Step request
	uid, err := opts.ServiceUID()
	if err != nil {
		log.Error().Err(err).Str("step", "request").Msg("Root update refused")
		return &RootApplyError{Step: "request", Err: err}
	}
	version, err := consumeRequest(opts.RequestDir, opts.RequestFile, uid, log)
	if err != nil {
		log.Error().Err(err).Str("step", "request").Msg("Root update refused")
		return &RootApplyError{Step: "request", Err: err}
	}
	log.Info().Str("version", version).Msg("Root update requested")

	// Step lock
	if err := os.MkdirAll(opts.RootStateDir, 0o700); err != nil {
		log.Error().Err(err).Str("step", "lock").Msg("Root update refused")
		return &RootApplyError{Step: "lock", Err: err}
	}
	release, err := acquireRootLock(opts.RootStateDir)
	if err != nil {
		log.Error().Err(err).Str("step", "lock").Msg("Root update refused")
		return &RootApplyError{Step: "lock", Err: err}
	}
	defer release()

	// Step list
	a := &autoUpdate{
		logger:        log,
		httpClient:    opts.HTTPClient,
		targetPath:    opts.Binary,
		stateDir:      opts.StateDir,
		seqDir:        opts.RootStateDir,
		downloadWait:  opts.DownloadWait,
		listRetryWait: opts.ListRetryWait,
	}
	registry := a.GetRegistryUrl(opts.Registry)
	list, err := a.admissibleVersions(registry, version)
	if err != nil {
		wrapped := fmt.Errorf("list could not be verified: %w", err)
		log.Error().Err(wrapped).Str("step", "list").Msg("Root update refused")
		return &RootApplyError{Step: "list", Err: wrapped}
	}

	// Step published
	if !publishes(list, version) {
		err := fmt.Errorf("version %s is not published in the signed list", version)
		log.Error().Err(err).Str("step", "published").Msg("Root update refused")
		return &RootApplyError{Step: "published", Err: err}
	}

	// Step current
	if canonicalVersion(version) == canonicalVersion(cliArgs.Version) {
		log.Info().Str("version", version).Msg("Requested version is already installed")
		return nil
	}

	// Step install
	url, err := a.GetBinaryUrl(registry, version)
	if err != nil {
		log.Error().Err(err).Str("step", "install").Str("reason", failureReason(err)).Msg("Root update refused")
		return &RootApplyError{Step: "install", Err: err}
	}
	if err := a.doUpdate(url, version); err != nil {
		log.Error().Err(err).Str("step", "install").Str("reason", failureReason(err)).Msg("Root update refused")
		return &RootApplyError{Step: "install", Err: err}
	}

	// Promote rollback record if applicable
	if rec, err := readRecordNoFollow(filepath.Join(opts.StateDir, rolledBackFileName)); err == nil && rec != nil &&
		rec.Outcome == OutcomeRequested &&
		canonicalVersion(rec.RestoredVersion) == canonicalVersion(version) &&
		canonicalVersion(rec.FailedVersion) == canonicalVersion(cliArgs.Version) {
		env := rollbackEnv{
			fs:       osRollbackFS{},
			stateDir: opts.StateDir,
			now:      time.Now,
			log:      log,
		}
		if err := env.writeRolledBackRecord(RolledBackRecord{
			FailedVersion:   rec.FailedVersion,
			RestoredVersion: rec.RestoredVersion,
			RolledBackAt:    rec.RolledBackAt,
			BootAttempts:    rec.BootAttempts,
			Outcome:         OutcomeRestored,
		}); err != nil {
			log.Warn().Err(err).Msg("Failed to promote rollback record")
		}
	}

	// Step restart (if configured)
	if opts.Restart != nil {
		msg, restartErr := opts.Restart(false)
		if restartErr != nil {
			log.Error().Err(restartErr).Str("step", "restart").Msg("Root update restart failed, attempting rollback")
			// Attempt rollback to previous version
			restoreErr := RestoreAfterFailedStart(opts.StateDir, opts.Binary, version, cliArgs.Version, log)
			if restoreErr != nil {
				log.Error().Err(restoreErr).Str("step", "restore").Msg("Rollback failed")
			} else {
				log.Info().Msg("Previous version restored")
			}
			// Restart after rollback
			_, restartAfterErr := opts.Restart(true)
			// Construct final error message
			restoreMsg := "previous version restored"
			if restoreErr != nil {
				restoreMsg = "restore failed: " + restoreErr.Error()
			}
			finalErr := fmt.Errorf("restart failed: %w; %s", restartErr, restoreMsg)
			if restartAfterErr != nil {
				log.Error().Err(restartAfterErr).Str("step", "restart").Msg("Restart after rollback failed")
				finalErr = fmt.Errorf("%w; restart after restore: %w", finalErr, restartAfterErr)
			}
			return &RootApplyError{Step: "restart", Err: finalErr}
		}
		log.Info().Str("message", msg).Msg("Restart completed")
	}

	log.Info().
		Str("from", cliArgs.Version).
		Str("to", version).
		Msg("Root update applied")
	return nil
}

// readRecordNoFollow reads the rollback record the service wrote. The file is
// in a directory the service user controls, so it is opened without following
// a link, must be a small regular file, and is never read blocking: a FIFO or
// /dev/zero put there cannot hang or exhaust a root process.
func readRecordNoFollow(path string) (*RolledBackRecord, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("reading the mode of %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size > 4096 {
		return nil, fmt.Errorf("%s is not a small regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var rec RolledBackRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &rec, nil
}
