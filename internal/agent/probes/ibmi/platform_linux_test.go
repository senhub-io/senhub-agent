//go:build linux

package ibmi

import "testing"

// TestPlatformGate_LinuxAllows pins the platform-gate behaviour on
// linux: the gate must never refuse construction. This is the twin of
// platform_other_test.go's refusal assertion — together they make the
// per-platform contract visible from a single test run on either OS.
func TestPlatformGate_LinuxAllows(t *testing.T) {
	t.Parallel()

	if !platformSupported {
		t.Fatal("platformSupported is false on a linux build — platform_linux.go is mis-tagged")
	}
	if err := platformGate(); err != nil {
		t.Fatalf("platformGate() returned %v on linux; expected nil", err)
	}
}
