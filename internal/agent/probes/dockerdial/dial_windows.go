//go:build windows

package dockerdial

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

// defaultAddress is the pipe Docker Engine and Docker Desktop both expose
// on Windows.
const defaultAddress = NamedPipePrefix + "./pipe/docker_engine"

func dialNamedPipe(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
