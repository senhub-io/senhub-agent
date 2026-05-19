//go:build darwin

package logger

// LogBaseDir returns the macOS-canonical log directory for system
// daemons. /Library/Logs/SenHub is what `Console.app` indexes by
// default, mirroring how Apple ships their own daemons' logs.
func LogBaseDir() string {
	return "/Library/Logs/SenHub"
}
