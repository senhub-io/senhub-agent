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
	Logger       *logger.ModuleLogger
	RequestDir   string
	RequestFile  string
	ServiceUID   func() (uint32, error)
	Registry     string
	HTTPClient   *http.Client
	Binary       string
	StateDir     string
	RootStateDir string
	Restart      func(startIfStopped bool) (string, error)
	// PackageName names the package that owns Binary; nil asks dpkg or rpm.
	PackageName   func(binary string) string
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
	rbErr := consumeRollbackRequest(opts.RequestDir, RootUpdateRollbackFile, uid, log)
	version, verErr := consumeRequest(opts.RequestDir, opts.RequestFile, uid, log)
	switch {
	case rbErr == nil:
		return rootRollback(opts)
	case !errors.Is(rbErr, unix.ENOENT):
		log.Error().Err(rbErr).Str("step", "request").Msg("Root update refused")
		return &RootApplyError{Step: "request", Err: rbErr}
	case verErr != nil:
		log.Error().Err(verErr).Str("step", "request").Msg("Root update refused")
		return &RootApplyError{Step: "request", Err: verErr}
	}
	log.Info().Str("version", version).Msg("Root update requested")

	// Step edition
	pkgName := packageNameOf
	if opts.PackageName != nil {
		pkgName = opts.PackageName
	}
	archive, err := archiveBaseForPackage(pkgName(opts.Binary))
	if err != nil {
		log.Error().Err(err).Str("step", "edition").Msg("Root update refused")
		return &RootApplyError{Step: "edition", Err: err}
	}

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
		archiveBase:   archive,
		downloadWait:  opts.DownloadWait,
		listRetryWait: opts.ListRetryWait,
	}
	// Steps current and downgrade come first: both are decided from the
	// version strings alone, so a lower or equal request is answered locally,
	// without contacting the registry.
	// Step current
	if canonicalVersion(version) == canonicalVersion(cliArgs.Version) {
		log.Info().Str("version", version).Msg("Requested version is already installed")
		return nil
	}

	// Step downgrade: the service decides allow_downgrade, so the root unit
	// cannot take its word for it. A lower version is installed with the
	// package manager.
	if cmp, cerr := compareSemver(version, cliArgs.Version); cerr != nil || cmp < 0 {
		err := fmt.Errorf("version %s is lower than the installed %s; a downgrade is done with the package manager", version, cliArgs.Version)
		if cerr != nil {
			err = fmt.Errorf("comparing %s with the installed %s: %w", version, cliArgs.Version, cerr)
		}
		log.Error().Err(err).Str("step", "downgrade").Msg("Root update refused")
		return &RootApplyError{Step: "downgrade", Err: err}
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

	if err := writeInstallRecord(opts.RootStateDir, cliArgs.Version, version); err != nil {
		log.Warn().Err(err).Msg("Could not record the install; a rollback request will be refused")
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

// readSmallJSON reads a file the service wrote into v. The file is in a
// directory the service user controls, so it is opened without following a
// link, must be a small regular file, and is never read blocking: a FIFO or
// /dev/zero put there cannot hang or exhaust a root process. absent reports a
// missing file.
func readSmallJSON(path string, v any) (absent bool, err error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return true, nil
		}
		return false, fmt.Errorf("opening %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return false, fmt.Errorf("reading the mode of %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size > 4096 {
		return false, fmt.Errorf("%s is not a small regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return false, nil
}

const installRecordFile = "last-install.json"

// installRecord is what the root unit knows of its last install, in a
// directory the service cannot write. A rollback request is honoured only
// against it.
type installRecord struct {
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
}

func writeInstallRecord(rootDir, from, to string) error {
	data, err := json.Marshal(installRecord{From: canonicalVersion(from), To: canonicalVersion(to), At: time.Now()})
	if err != nil {
		return fmt.Errorf("encoding the install record: %w", err)
	}
	return osRollbackFS{}.WriteFileAtomic(filepath.Join(rootDir, installRecordFile), data)
}

// rootRollback puts back the binary this unit replaced. It downloads nothing
// and takes no version, so the service can ask for it but cannot steer it. It
// is honoured only when all of this holds: the unit itself made the last
// install, in its own root-owned record; that install is the running version
// and is less than rollbackWindow old; and the boot guard of that install is
// still pending after its allowed boots, which the service's own marker
// shows. The version it goes back to is the one in the record, never one the
// service names. The record is spent by the rollback.
func rootRollback(opts RootApplyOptions) error {
	log := opts.Logger
	refuse := func(err error) error {
		log.Error().Err(err).Str("step", "rollback").Msg("Root update refused")
		return &RootApplyError{Step: "rollback", Err: err}
	}
	if err := os.MkdirAll(opts.RootStateDir, 0o700); err != nil {
		return refuse(err)
	}
	release, err := acquireRootLock(opts.RootStateDir)
	if err != nil {
		return refuse(err)
	}
	defer release()

	path := filepath.Join(opts.RootStateDir, installRecordFile)
	data, err := os.ReadFile(path) // #nosec G304 - root's own state directory
	if err != nil {
		return refuse(fmt.Errorf("no install of this unit to go back from: %w", err))
	}
	var rec installRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return refuse(fmt.Errorf("reading the install record: %w", err))
	}
	switch {
	case rec.From == "" || canonicalVersion(rec.To) != canonicalVersion(cliArgs.Version):
		return refuse(fmt.Errorf("the last install of this unit (%s) is not the running version %s", rec.To, cliArgs.Version))
	case time.Since(rec.At) > rollbackWindow || time.Since(rec.At) < 0:
		return refuse(fmt.Errorf("the last install is older than %s", rollbackWindow))
	}
	var pending pendingUpdate
	absent, err := readSmallJSON(filepath.Join(opts.StateDir, pendingFileName), &pending)
	switch {
	case err != nil:
		return refuse(fmt.Errorf("reading the boot guard marker: %w", err))
	case absent:
		return refuse(fmt.Errorf("the boot guard of %s is not pending: the update was confirmed or never made", rec.To))
	case canonicalVersion(pending.ToVersion) != canonicalVersion(rec.To) || pending.BootAttempts < maxBootAttempts:
		return refuse(fmt.Errorf("the boot guard of %s has not used its %d boots", rec.To, maxBootAttempts))
	}
	if err := os.Remove(path); err != nil {
		return refuse(fmt.Errorf("consuming the install record: %w", err))
	}
	log.Info().Str("from", rec.To).Str("to", rec.From).Msg("Root rollback requested")
	if err := RestoreAfterFailedStart(opts.StateDir, opts.Binary, rec.To, rec.From, log); err != nil {
		return refuse(err)
	}
	if opts.Restart != nil {
		if _, err := opts.Restart(true); err != nil {
			log.Error().Err(err).Str("step", "restart").Msg("Restart after rollback failed")
			return &RootApplyError{Step: "restart", Err: err}
		}
	}
	log.Info().Str("from", rec.To).Str("to", rec.From).Msg("Root rollback applied")
	return nil
}

const (
	packageFull = "senhub-agent"
	packageOSS  = "senhub-agent-oss"
)

// archiveBaseForPackage maps the package that owns the binary to the archive
// family it must be updated from: the open source package only from the open
// source archive, never from the full edition one, and the other way round.
func archiveBaseForPackage(name string) (string, error) {
	switch name {
	case packageOSS:
		return "senhub-agent-oss", nil
	case packageFull:
		return "senhub-agent", nil
	}
	return "", fmt.Errorf("the edition of this install cannot be told: the binary belongs to package %q", name)
}
