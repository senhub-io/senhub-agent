package dockerdial

import "testing"

// TestIsNamedPipe covers the three spellings an operator may write, and
// the Unix socket that must not be mistaken for one.
func TestIsNamedPipe(t *testing.T) {
	pipes := []string{
		"npipe://./pipe/docker_engine",
		`\\.\pipe\docker_engine`,
		"//./pipe/docker_engine",
	}
	for _, addr := range pipes {
		if !IsNamedPipe(addr) {
			t.Errorf("%q not recognised as a named pipe", addr)
		}
	}
	for _, addr := range []string{"/var/run/docker.sock", "/run/user/1000/docker.sock", ""} {
		if IsNamedPipe(addr) {
			t.Errorf("%q wrongly taken for a named pipe", addr)
		}
	}
}

// TestPipePath normalises every accepted spelling onto the one form the
// Windows API takes, so a config written with forward slashes still works.
func TestPipePath(t *testing.T) {
	want := `\\.\pipe\docker_engine`
	for _, addr := range []string{
		"npipe://./pipe/docker_engine",
		`\\.\pipe\docker_engine`,
		"//./pipe/docker_engine",
		"npipe://docker_engine",
	} {
		if got := PipePath(addr); got != want {
			t.Errorf("PipePath(%q)=%q, want %q", addr, got, want)
		}
	}
}

// TestDefaultAddress is the whole point of the change: the default must
// name the transport this platform actually exposes.
func TestDefaultAddress(t *testing.T) {
	if DefaultAddress() == "" {
		t.Fatal("no default engine address for this platform")
	}
}
