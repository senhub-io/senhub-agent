// Package datadir keeps the Windows application-data directory
// (%ProgramData%\SenHub) readable by SYSTEM and Administrators only.
package datadir

import (
	"fmt"
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
	apply(path string) error
}

// NeedsRepair reports whether the DACL is weaker than ProtectedSDDL and why.
func NeedsRepair(s Snapshot) (bool, string) {
	if !s.Protected {
		return true, "inheritance from the parent is not disabled"
	}
	for _, ace := range s.Allowed {
		switch strings.ToUpper(ace.SID) {
		case sidEveryone, sidAuthenticatedUsers, sidBuiltinUsers:
			return true, fmt.Sprintf("access granted to %s", ace.SID)
		}
	}
	return false, ""
}

// ensure applies ProtectedSDDL to dir when its current DACL needs repair and
// reports whether it changed anything.
func ensure(b backend, dir string, log Logger) (bool, error) {
	snap, err := b.inspect(dir)
	if err != nil {
		return false, fmt.Errorf("inspecting ACL of %s: %w", dir, err)
	}
	repair, reason := NeedsRepair(snap)
	if !repair {
		return false, nil
	}
	if err := b.apply(dir); err != nil {
		return false, fmt.Errorf("restricting ACL of %s: %w", dir, err)
	}
	log.Info().Str("dir", dir).Str("reason", reason).Msg("Restricted data directory ACL to SYSTEM and Administrators")
	return true, nil
}
