// Package dockerdial opens a connection to the Docker Engine API on the
// transport the host actually offers.
//
// The engine listens on a Unix socket on Linux and macOS, and on a named
// pipe on Windows (\\.\pipe\docker_engine). The probes dialled a Unix
// socket unconditionally, so on Windows they reached nothing and a host
// running containers was simply invisible (#801).
package dockerdial

import (
	"context"
	"net"
	"strings"
)

// NamedPipePrefix marks an address as a Windows named pipe. Operators can
// write it explicitly in socket_path; on Windows it is also the default.
const NamedPipePrefix = "npipe://"

// DefaultAddress is where the Docker Engine listens on this platform.
func DefaultAddress() string { return defaultAddress }

// IsNamedPipe reports whether addr names a Windows named pipe, either by
// the npipe:// scheme or by the \\.\pipe\ path form.
func IsNamedPipe(addr string) bool {
	return strings.HasPrefix(addr, NamedPipePrefix) ||
		strings.HasPrefix(addr, `\\.\pipe\`) ||
		strings.HasPrefix(addr, "//./pipe/")
}

// PipePath strips the npipe:// scheme and normalises separators, giving
// the path form the Windows API expects.
func PipePath(addr string) string {
	p := strings.TrimPrefix(addr, NamedPipePrefix)
	p = strings.ReplaceAll(p, "/", `\`)
	// Every accepted spelling collapses onto the one form the Windows API
	// takes. The name is whatever follows the pipe root, so strip any
	// leading `.\pipe\` an operator wrote as part of the address.
	p = strings.TrimPrefix(p, `\\.\pipe\`)
	p = strings.TrimPrefix(p, `.\pipe\`)
	p = strings.TrimPrefix(p, `\`)
	return `\\.\pipe\` + p
}

// Dial connects to the engine at addr: a named pipe when addr names one
// and the platform supports it, a Unix socket otherwise.
func Dial(ctx context.Context, addr string) (net.Conn, error) {
	if IsNamedPipe(addr) {
		return dialNamedPipe(ctx, PipePath(addr))
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", addr)
}
