package otlpreceiver

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
)

// listen opens the configured listener: TCP, or a Unix domain socket when
// the address says so. A socket file left by a previous run is removed
// first, since binding over it fails; any other file at that path is left
// alone and reported.
func (p *OTLPReceiverProbe) listen() (net.Listener, error) {
	if p.config.UnixPath == "" {
		lis, err := net.Listen("tcp", p.config.Address)
		if err != nil {
			return nil, fmt.Errorf("listening on %s: %w", p.config.Address, err)
		}
		return lis, nil
	}
	path := p.config.UnixPath
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&fs.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("removing the stale socket %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspecting %s: %w", path, err)
	}
	lis, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on the unix socket %s: %w", path, err)
	}
	// Any local program may send, as any local program may reach a
	// loopback port; a bearer_token still applies. The socket exists so
	// the sender is known to be on this machine, not to restrict who.
	if err := os.Chmod(path, 0o666); err != nil { // #nosec G302 -- a local ingest socket, writable by local senders on purpose
		return nil, errors.Join(fmt.Errorf("opening %s to local senders: %w", path, err), lis.Close())
	}
	return lis, nil
}
