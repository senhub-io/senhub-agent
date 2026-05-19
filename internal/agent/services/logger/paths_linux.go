//go:build linux

package logger

// LogBaseDir returns the FHS-canonical log directory for the agent on
// Linux. The pre-0.2.0 path was /var/log/senhub; we moved to the
// app-specific suffix so an admin running multiple SenHub binaries
// (agent + future intake-relay + …) keeps their logs cleanly
// separated.
func LogBaseDir() string {
	return "/var/log/senhub-agent"
}
