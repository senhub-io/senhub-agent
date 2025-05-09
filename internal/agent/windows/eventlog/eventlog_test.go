//go:build !windows

package eventlog

import (
	"testing"
)

// TestSkip skips tests for non-Windows platforms
func TestSkip(t *testing.T) {
	t.Skip("Skipping Windows Event Log tests on non-Windows platform")
}