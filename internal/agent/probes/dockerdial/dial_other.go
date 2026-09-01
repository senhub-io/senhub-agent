//go:build !windows

package dockerdial

import (
	"context"
	"fmt"
	"net"
)

// defaultAddress is the socket the engine exposes on Unix platforms.
const defaultAddress = "/var/run/docker.sock"

// dialNamedPipe exists so the address forms parse identically on every
// platform: a configuration naming a pipe is refused here with a clear
// reason rather than failing later as an unreachable Unix socket.
func dialNamedPipe(_ context.Context, path string) (net.Conn, error) {
	return nil, fmt.Errorf("named pipe %q is a Windows transport; this platform reaches the Docker Engine over a Unix socket", path)
}
