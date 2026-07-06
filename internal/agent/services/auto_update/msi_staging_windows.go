//go:build windows

package auto_update

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// stagingBaseSDDL locks the staging base to SYSTEM and Administrators with full
// control, protected (P) so it inherits nothing from %ProgramData% — whose stock
// ACL grants BUILTIN\Users create-folder rights. Without this a local non-admin
// could pre-create the base, own it (CREATOR OWNER), and gain FILE_DELETE_CHILD
// over the SYSTEM-created random staging dir, re-opening the verify-then-swap
// TOCTOU the per-run directory is meant to close (M3).
const stagingBaseSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// secureStageBase creates the staging base with a restrictive DACL, or — if it
// already exists — verifies it is owned by a trusted principal before use.
//
// Go's 0o700 mode is a no-op on Windows (directories inherit the parent DACL),
// and os.MkdirAll succeeds silently on a pre-existing attacker-owned directory,
// so the anti-TOCTOU staging needs an explicit ACL on create and an ownership
// check on reuse. A base owned by anyone other than SYSTEM, Administrators, or
// the agent's own identity is refused loudly rather than trusted (M3).
func secureStageBase(baseDir string) error {
	if fi, err := os.Stat(baseDir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("staging base %s exists but is not a directory", baseDir)
		}
		return verifyStageBaseOwner(baseDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat staging base %s: %w", baseDir, err)
	}

	// Create the parent chain first (inherits %ProgramData% ACL, acceptable —
	// only the base itself must be locked down), then the base with os.Mkdir so
	// a racing pre-creation fails our create loudly instead of being trusted.
	if err := os.MkdirAll(filepath.Dir(baseDir), 0o700); err != nil {
		return fmt.Errorf("creating staging base parent of %s: %w", baseDir, err)
	}
	if err := os.Mkdir(baseDir, 0o700); err != nil {
		return fmt.Errorf("creating staging base %s: %w", baseDir, err)
	}
	if err := applyStageBaseDACL(baseDir); err != nil {
		// Do not leave a base with an inherited (too-permissive) ACL behind.
		_ = os.RemoveAll(baseDir)
		return err
	}
	return nil
}

// applyStageBaseDACL replaces the directory's DACL with the SYSTEM/Administrators
// protected DACL parsed from stagingBaseSDDL.
func applyStageBaseDACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString(stagingBaseSDDL)
	if err != nil {
		return fmt.Errorf("parsing staging DACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("extracting staging DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("applying staging DACL to %s: %w", path, err)
	}
	return nil
}

// verifyStageBaseOwner refuses a pre-existing staging base unless it is owned by
// SYSTEM, Administrators, or the agent's own identity — the only principals that
// could not be a lower-privileged attacker who pre-created it.
func verifyStageBaseOwner(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("reading owner of staging base %s: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("extracting owner of staging base %s: %w", path, err)
	}

	trusted := make([]*windows.SID, 0, 3)
	if sid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid); err == nil {
		trusted = append(trusted, sid)
	}
	if sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid); err == nil {
		trusted = append(trusted, sid)
	}
	if user, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		trusted = append(trusted, user.User.Sid)
	}

	for _, sid := range trusted {
		if owner.Equals(sid) {
			return nil
		}
	}
	return fmt.Errorf("staging base %s is owned by untrusted principal %s; refusing to stage MSI here", path, owner.String())
}
