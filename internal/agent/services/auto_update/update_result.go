package auto_update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	updateResultFile = "update-result.json"

	ResultInstalled           = "installed"
	ResultRefused             = "refused"
	ResultRolledBack          = "rolled_back"
	ReasonRateLimited         = "rate_limited"
	ReasonRequestNotRemovable = "request_not_removable"
	maxResultBytes            = 4096
)

// UpdateResult is what the root update unit reports of its last run, in the
// service's state directory: the version asked for, what became of it, why not
// when it was refused, and when. The file is public (0644); the unit writes
// it, the service only reads it.
type UpdateResult struct {
	Version string    `json:"version"`
	Outcome string    `json:"outcome"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// ReadUpdateResult returns the last result in stateDir, nil when there is none.
func ReadUpdateResult(stateDir string) (*UpdateResult, error) {
	f, err := os.Open(filepath.Join(stateDir, updateResultFile)) // #nosec G304 - the agent's own state directory
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the update result: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxResultBytes))
	if err != nil {
		return nil, fmt.Errorf("reading the update result: %w", err)
	}
	var r UpdateResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing the update result: %w", err)
	}
	return &r, nil
}

const (
	resultPollEvery = 2 * time.Second
	resultWatchFor  = 20 * time.Minute
)

// watchRootResult follows a request handed to the privileged path until the
// unit reports on it, and turns a refusal into the updater status and one
// warning, which is what a fleet server sees. A success needs nothing: the
// service restarts on the new version.
func (a *autoUpdate) watchRootResult(target string, requestedAt time.Time) {
	every, until := resultPollEvery, resultWatchFor
	if a.resultPoll != nil {
		every = *a.resultPoll
	}
	stateDir := a.rollbackEnvFor("").stateDir
	go func() {
		deadline := time.Now().Add(until)
		for time.Now().Before(deadline) {
			time.Sleep(every)
			r, err := ReadUpdateResult(stateDir)
			if err != nil || r == nil || r.At.Before(requestedAt.Add(-2*time.Second)) || (r.Version != "" && canonicalVersion(r.Version) != canonicalVersion(target)) {
				continue
			}
			switch r.Outcome {
			case ResultRefused:
				setStatus(Status{State: StateFailed, Target: target, Reason: r.Reason})
				a.logger.Warn().Str("target", target).Str("reason", r.Reason).Msg("The root update unit refused the update")
			case ResultRolledBack:
				setStatus(Status{State: StateFailed, Target: target, Reason: r.Reason})
				a.logger.Warn().Str("target", target).Str("reason", r.Reason).Msg("The root update unit restored the previous version")
			}
			return
		}
	}()
}
