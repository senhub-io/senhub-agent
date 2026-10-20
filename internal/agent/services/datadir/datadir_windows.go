//go:build windows

package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Secure restricts dir to SYSTEM and Administrators when dir is the standard
// %ProgramData%\SenHub directory; any other location (a custom --config-path)
// is the operator's to manage and is left untouched. The directory is created
// if missing. The service runs as LocalSystem, so it keeps full control.
func Secure(dir string, log Logger) error {
	if !isStandardDir(dir) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	_, err := ensure(winBackend{}, dir, log)
	return err
}

func isStandardDir(dir string) bool {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return strings.EqualFold(filepath.Clean(dir), filepath.Join(programData, "SenHub"))
}

type winBackend struct{}

func (winBackend) inspect(path string) (Snapshot, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return Snapshot{}, fmt.Errorf("reading security descriptor: %w", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return Snapshot{}, fmt.Errorf("reading descriptor control: %w", err)
	}
	snap := Snapshot{Protected: control&windows.SE_DACL_PROTECTED != 0}

	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		// A missing DACL grants everyone full access.
		snap.Allowed = append(snap.Allowed, ACE{SID: sidEveryone})
		return snap, nil
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return Snapshot{}, fmt.Errorf("reading ACE %d: %w", i, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		snap.Allowed = append(snap.Allowed, ACE{SID: sid.String(), Mask: uint32(ace.Mask)})
	}
	return snap, nil
}

func (winBackend) apply(path string) error {
	sd, err := windows.SecurityDescriptorFromString(ProtectedSDDL)
	if err != nil {
		return fmt.Errorf("parsing SDDL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("extracting DACL: %w", err)
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	)
}
