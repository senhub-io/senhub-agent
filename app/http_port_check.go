package app

import (
	"fmt"
	"net"
	"strconv"
)

// Defaults of the generated HTTP strategy fragment, mirrored from the
// configuration generator so the pre-flight probe tests the address the
// agent will actually bind.
const (
	defaultHTTPPort        = 8080
	defaultHTTPBindAddress = "127.0.0.1"
)

// checkHTTPPortFree binds the address the generated HTTP strategy will
// listen on and releases it. It answers the one question an installer
// can answer before the service starts: will the agent be reachable?
// A taken port otherwise produces an install that exits 0, a service
// that runs, and an interface that never answers, with the only trace
// being one line in the log.
//
// The probe uses the same listen call as the strategy, so it predicts
// the strategy's own outcome, including the Windows case where another
// process holds the port without exclusive use.
func checkHTTPPortFree(bindAddress string, port int) error {
	addr := net.JoinHostPort(bindAddress, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is not available on %s (%v); choose another with --http-port", port, bindAddress, err)
	}
	return ln.Close()
}
