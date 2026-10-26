// Package datadir keeps the Windows application-data directory
// (%ProgramData%\SenHub) readable by SYSTEM and Administrators only.
package datadir

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/rs/zerolog"
)

// ProtectedSDDL grants SYSTEM and BUILTIN\Administrators full control, inherited
// by files and subfolders (OI)(CI), and blocks inheritance from %ProgramData%
// (P). The stock %ProgramData% ACL gives BUILTIN\Users read and create rights on
// subfolders, which would expose agent.yaml, the sealed secret store and the
// licence to any local account.
const ProtectedSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

const (
	sidEveryone           = "S-1-1-0"
	sidAuthenticatedUsers = "S-1-5-11"
	sidBuiltinUsers       = "S-1-5-32-545"
)

// ACE is the part of an access-allowed entry the audit needs.
type ACE struct {
	SID  string
	Mask uint32
}

// Snapshot is the audited view of a directory DACL.
type Snapshot struct {
	Protected bool
	Allowed   []ACE
}

// Logger is the subset of the module logger used here.
type Logger interface {
	Info() *zerolog.Event
}

type backend interface {
	inspect(path string) (Snapshot, error)
	list(root string) ([]string, error)
	inherit(path string) error
	apply(path string) error
}

// grantsBroadAccess reports whether any allowed ACE grants access to a broad
// local principal.
func grantsBroadAccess(s Snapshot) (bool, string) {
	for _, ace := range s.Allowed {
		switch strings.ToUpper(ace.SID) {
		case sidEveryone, sidAuthenticatedUsers, sidBuiltinUsers:
			return true, fmt.Sprintf("access granted to %s", ace.SID)
		}
	}
	return false, ""
}

// NeedsRepair reports whether the DACL is weaker than ProtectedSDDL and why.
func NeedsRepair(s Snapshot) (bool, string) {
	if !s.Protected {
		return true, "inheritance from the parent is not disabled"
	}
	return grantsBroadAccess(s)
}

// ensureTree repairs root and every descendant when root or any descendant
// grants access to a broad local principal. It returns the number of paths
// rewritten.
func ensureTree(b backend, root string, log Logger) (int, error) {
	rootSnap, err := b.inspect(root)
	if err != nil {
		return 0, fmt.Errorf("inspecting ACL of %s: %w", root, err)
	}
	descendants, err := b.list(root)
	if err != nil {
		return 0, fmt.Errorf("listing descendants of %s: %w", root, err)
	}

	repair, reason := NeedsRepair(rootSnap)
	if !repair {
		for _, path := range descendants {
			snap, err := b.inspect(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return 0, fmt.Errorf("inspecting ACL of %s: %w", path, err)
			}
			if broad, broadReason := grantsBroadAccess(snap); broad {
				repair = true
				reason = broadReason
				break
			}
		}
	}
	if !repair {
		return 0, nil
	}

	if err := b.apply(root); err != nil {
		return 0, fmt.Errorf("restricting ACL of %s: %w", root, err)
	}
	for _, path := range descendants {
		if err := b.inherit(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("resetting ACL inheritance on %s: %w", path, err)
		}
	}

	count := 1 + len(descendants)
	log.Info().
		Str("dir", root).
		Int("paths", count).
		Str("reason", reason).
		Msg("Restricted data directory ACL to SYSTEM and Administrators")
	return count, nil
}
