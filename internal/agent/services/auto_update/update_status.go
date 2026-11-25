package auto_update

import (
	"errors"
	"sync"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/instanceid"
)

// Update states reported to a server that drives the update.
const (
	StateIdle        = "idle"
	StateChecking    = "checking"
	StateDownloading = "downloading"
	StateInstalling  = "installing"
	StateFailed      = "failed"
	StateRolledBack  = "rolled_back"
)

// Failure reasons that are not a refusal code.
const (
	ReasonDownload   = "download"
	ReasonSignature  = "signature"
	ReasonInstall    = "install"
	ReasonNotReplace = "not_replaceable"
)

// Status is what the updater reports about itself: the state, the version
// aimed at (empty when none), and the reason of a failure.
type Status struct {
	State  string
	Target string
	Reason string
}

var (
	statusMu        sync.Mutex
	statusCurrent   Status
	statusKnown     bool
	statusListeners []func()
)

// CurrentStatus returns the updater state. Before the first change it is
// derived from the rollback record: an agent that came back from a failed
// start reports rolled_back until another version is installed.
func CurrentStatus() Status {
	statusMu.Lock()
	defer statusMu.Unlock()
	if !statusKnown {
		statusCurrent = baselineStatus(instanceid.OwnStateDir())
		statusKnown = true
	}
	return statusCurrent
}

// OnStatusChange registers fn, called after each change of the status. It
// runs on its own goroutine and reads the status again itself.
func OnStatusChange(fn func()) {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusListeners = append(statusListeners, fn)
}

// notifyStatusListeners wakes every listener without changing the status,
// for facts a server learns by reading the status payload again (for
// instance self-replaceability).
func notifyStatusListeners() {
	statusMu.Lock()
	listeners := append([]func(){}, statusListeners...)
	statusMu.Unlock()
	for _, fn := range listeners {
		go fn()
	}
}

func setStatus(s Status) {
	statusMu.Lock()
	if !statusKnown {
		statusKnown = true
	} else if statusCurrent == s {
		statusMu.Unlock()
		return
	}
	statusCurrent = s
	listeners := append([]func(){}, statusListeners...)
	statusMu.Unlock()
	for _, fn := range listeners {
		go fn()
	}
}

// settleStatus puts the status back to what the rollback record says once a
// cycle ends without a failure of its own.
func settleStatus(stateDir string) {
	setStatus(baselineStatus(stateDir))
}

// baselineStatus is idle, or rolled_back while the running version is the one
// a failed start was restored to.
func baselineStatus(stateDir string) Status {
	if stateDir == "" {
		return Status{State: StateIdle}
	}
	rec, err := readRolledBack(osRollbackFS{}, stateDir)
	if err != nil || rec == nil || rec.Outcome != OutcomeRestored {
		return Status{State: StateIdle}
	}
	if canonicalVersion(rec.RestoredVersion) != canonicalVersion(cliArgs.Version) {
		return Status{State: StateIdle}
	}
	return Status{State: StateRolledBack, Target: rec.FailedVersion, Reason: "rolled_back:" + rec.FailedVersion}
}

// resetStatusForTest forgets the status and its listeners. Tests only.
func resetStatusForTest() {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusCurrent, statusKnown, statusListeners = Status{}, false, nil
}

// failure carries the reason code of a failed update attempt.
type failure struct {
	reason string
	err    error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

func fail(reason string, err error) error {
	if err == nil {
		return nil
	}
	return &failure{reason: reason, err: err}
}

func failureReason(err error) string {
	var f *failure
	if errors.As(err, &f) {
		return f.reason
	}
	return ReasonInstall
}
