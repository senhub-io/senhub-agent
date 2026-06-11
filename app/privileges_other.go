//go:build !windows

package app

// isElevated is only meaningful on Windows; checkPrivileges never
// reaches it on other platforms.
func isElevated() bool { return false }
