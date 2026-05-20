//go:build windows

package logger

import (
	"os"
	"path/filepath"
)

// LogBaseDir returns %ProgramData%\SenHub\logs on Windows — the
// MSDN-recommended location for shared application data that
// survives across user accounts and Windows service runs.
func LogBaseDir() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "SenHub", "logs")
}
