package auto_update

import (
	"strings"
	"testing"
)

func TestWritabilityPreflight(t *testing.T) {
	const exe = "/usr/bin/senhub-agent"

	t.Run("writable binary and dir → no warning", func(t *testing.T) {
		got := WritabilityPreflight(exe, func(string) bool { return true })
		if got != "" {
			t.Errorf("expected no warning when writable, got %q", got)
		}
	})

	t.Run("read-only binary → actionable warning", func(t *testing.T) {
		got := WritabilityPreflight(exe, func(string) bool { return false })
		if got == "" {
			t.Fatal("expected a warning when the binary is not writable")
		}
		if !strings.Contains(got, exe) {
			t.Errorf("warning should name the binary path, got %q", got)
		}
		if !strings.Contains(got, "auto_update.enabled") {
			t.Errorf("warning should mention auto_update.enabled, got %q", got)
		}
	})

	t.Run("writable file but read-only dir → warning", func(t *testing.T) {
		// selfupdate renames a sibling over the binary, so the directory
		// must be writable too — a writable file in a read-only dir still
		// cannot be replaced.
		got := WritabilityPreflight(exe, func(p string) bool { return p == exe })
		if got == "" {
			t.Error("expected a warning when the parent directory is not writable")
		}
	})

	t.Run("empty exe path → no warning", func(t *testing.T) {
		if got := WritabilityPreflight("", func(string) bool { return false }); got != "" {
			t.Errorf("expected no warning for an empty exe path, got %q", got)
		}
	})
}
