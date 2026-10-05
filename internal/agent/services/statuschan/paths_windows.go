//go:build windows

package statuschan

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

const pipeName = `\\.\pipe\senhub-agent-status`

// pipeSecurity grants the pipe to LocalSystem and the local
// administrators only, and blocks inheritance (P): the Windows
// counterpart of a 0600 socket owned by the service account.
const pipeSecurity = "D:P(A;;GA;;;SY)(A;;GA;;;BA)"

func candidatePaths(string) []string { return []string{pipeName} }

func listen(paths []string) (net.Listener, string, error) {
	// With zero-sized buffers a pipe write completes only once the other
	// side reads, so a client that writes before reading and a server that
	// never reads wait on each other until the write deadline.
	ln, err := winio.ListenPipe(paths[0], &winio.PipeConfig{
		SecurityDescriptor: pipeSecurity,
		InputBufferSize:    4096,
		OutputBufferSize:   65536,
	})
	if err != nil {
		return nil, "", err
	}
	return ln, paths[0], nil
}

func dial(path string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(path, &timeout)
}

func cleanup(string) {}
