//go:build windows

package cliArgs

import (
	"os"
	"path/filepath"
)

// canonicalConfigPathForOS returns %ProgramData%\SenHub\agent.yaml on
// Windows — the MSDN-recommended location for shared application
// configuration that's preserved across user accounts.
func canonicalConfigPathForOS() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "SenHub", "agent.yaml")
}
