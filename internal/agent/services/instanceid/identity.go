package instanceid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/fsown"
)

// IdentityFileName records, in the state directory, which host the agent key
// was last seen on. It carries no secret, only the derived instance id and
// the host id, so that a machine cloned after its installation can be told
// from the copy's own state directory.
const IdentityFileName = "identity.json"

// ConnectedElsewhereFileName marks a state directory whose identity Fleet
// refused because the original machine was already connected with it. It
// survives restarts so `agent doctor` can still report the refusal.
const ConnectedElsewhereFileName = "connected-elsewhere.json"

// IdentityRecord is the non-secret footprint one agent key leaves on a host.
type IdentityRecord struct {
	InstanceID string    `json:"instance_id"`
	HostID     string    `json:"host_id"`
	RecordedAt time.Time `json:"recorded_at"`
}

// IdentityStatus is what CheckIdentity concluded about the recorded identity.
type IdentityStatus int

const (
	// IdentityUnchecked means the host id is unknown, so nothing was compared
	// and nothing was written.
	IdentityUnchecked IdentityStatus = iota
	// IdentityRecorded means no record existed and one was created.
	IdentityRecorded
	// IdentityMatches means the same instance was already recorded on this
	// host: the normal case.
	IdentityMatches
	// IdentityRekeyed means the instance differs, so the key changed since
	// the record was written, and the record was rewritten.
	IdentityRekeyed
	// IdentityHostChanged means the same instance was recorded on another
	// host: the configuration was copied from that machine. The record is
	// deliberately left alone, so the original machine keeps its evidence.
	IdentityHostChanged
)

// normalizeHostID makes host id comparison insensitive to case and padding:
// resource attributes are collected, not authored, and both forms reach here.
func normalizeHostID(hostID string) string {
	return strings.ToLower(strings.TrimSpace(hostID))
}

// CheckIdentity compares the agent's instance id and host id against the
// record in dir. It returns the record as it stood before the call, nil when
// none existed. A missing host id means nothing can be compared, so nothing
// is written. An unreadable or corrupt record is an error and is never
// overwritten: the operator should look at it, not lose it.
func CheckIdentity(dir, instanceID, hostID string, now time.Time) (IdentityStatus, *IdentityRecord, error) {
	if instanceID == "" {
		return IdentityUnchecked, nil, errors.New("empty instance id")
	}
	existing, err := ReadIdentity(dir)
	if err != nil {
		return IdentityUnchecked, nil, err
	}
	hostID = normalizeHostID(hostID)
	if existing == nil {
		if hostID == "" {
			return IdentityUnchecked, nil, nil
		}
		rec := &IdentityRecord{InstanceID: instanceID, HostID: hostID, RecordedAt: now}
		if err := writeJSONAtomic(filepath.Join(dir, IdentityFileName), rec); err != nil {
			return IdentityUnchecked, nil, err
		}
		return IdentityRecorded, nil, nil
	}
	if existing.InstanceID != instanceID {
		rec := &IdentityRecord{InstanceID: instanceID, HostID: hostID, RecordedAt: now}
		if err := writeJSONAtomic(filepath.Join(dir, IdentityFileName), rec); err != nil {
			return IdentityUnchecked, existing, err
		}
		return IdentityRekeyed, existing, nil
	}
	// A record without a host id (written while the host id was unknown) is
	// not evidence of a copy, so it is compared against nothing.
	if hostID == "" || normalizeHostID(existing.HostID) == "" {
		return IdentityUnchecked, existing, nil
	}
	if normalizeHostID(existing.HostID) == hostID {
		return IdentityMatches, existing, nil
	}
	return IdentityHostChanged, existing, nil
}

// ReadIdentity returns the record in dir, or nil, nil when the file is
// absent. Anything else on disk is an error the caller can report.
func ReadIdentity(dir string) (*IdentityRecord, error) {
	return readRecord[IdentityRecord](filepath.Join(dir, IdentityFileName))
}

// ConnectedElsewhere remembers when Fleet refused this agent because its
// identity was already connected from another machine, and why.
type ConnectedElsewhere struct {
	At     time.Time `json:"at"`
	Detail string    `json:"detail,omitempty"`
}

// RecordConnectedElsewhere writes the refusal marker in dir.
func RecordConnectedElsewhere(dir string, at time.Time, detail string) error {
	return writeJSONAtomic(filepath.Join(dir, ConnectedElsewhereFileName), ConnectedElsewhere{At: at, Detail: detail})
}

// ReadConnectedElsewhere returns the refusal marker in dir, or nil, nil when
// none was ever written.
func ReadConnectedElsewhere(dir string) (*ConnectedElsewhere, error) {
	return readRecord[ConnectedElsewhere](filepath.Join(dir, ConnectedElsewhereFileName))
}

// ClearConnectedElsewhere drops the refusal marker once it has been handled.
// A missing file is success: there was nothing to clear.
func ClearConnectedElsewhere(dir string) error {
	if err := os.Remove(filepath.Join(dir, ConnectedElsewhereFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// readRecord decodes the JSON record at path, mapping a missing file to a
// nil record so callers can tell "never recorded" from "record broken".
func readRecord[T any](path string) (*T, error) {
	raw, err := os.ReadFile(path) // #nosec G304 - the agent's own state directory
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rec T
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s is not a valid record: %w", path, err)
	}
	return &rec, nil
}

// writeJSONAtomic writes v to path through a temporary file and a rename, so
// a reader never sees a partial record. Mode 0o644: the records hold no
// secret, and an operator copying a state directory to diagnose a clone
// should be able to read them.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing record file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil && !fsown.Refused(err) {
		cleanup()
		return fmt.Errorf("setting mode on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("publishing %s: %w", path, err)
	}
	return nil
}
