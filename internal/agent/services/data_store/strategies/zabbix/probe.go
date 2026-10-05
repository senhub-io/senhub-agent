package zabbix

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// HostState is what the server said about this host's name.
type HostState int

const (
	// HostNotChecked: no answer to the check-list request was read.
	HostNotChecked HostState = iota
	// HostKnown: the server returned a check list for the host.
	HostKnown
	// HostUnknown: the server answered that it has no such host. Normal
	// before autoregistration has run; not a connectivity failure.
	HostUnknown
	// HostRefused: the server answered "failed" for another reason.
	HostRefused
)

// ProtocolCheck is the outcome of one active-checks exchange with one
// server address, as the connection test of the console reports it.
type ProtocolCheck struct {
	Hostname string
	// Encryption is "PSK", "TLS" or "" when the connection is in clear.
	Encryption string
	// HandshakeErr is set when the encryption handshake was refused or
	// failed on a reachable socket.
	HandshakeErr error
	// Answered says the peer replied with a valid Zabbix frame.
	Answered bool
	// ProtocolErr is set when no valid Zabbix reply came back.
	ProtocolErr error
	Host        HostState
	// HostInfo is the server's own text for an unknown or refused host.
	HostInfo string
}

// CheckProtocol sends the request the agent sends at startup (an active
// checks request for the configured host name) to addr and classifies the
// reply. The request is the one that triggers autoregistration on the
// server, so it has the same effect as the agent starting. dial opens the
// raw TCP connection; the encryption handshake runs on top of it.
func CheckProtocol(ctx context.Context, params map[string]interface{}, addr string, dial func(ctx context.Context, network, address string) (net.Conn, error)) (ProtocolCheck, error) {
	cfg, err := ParseConfig(params)
	if err != nil {
		return ProtocolCheck{}, err
	}
	cfg.Servers = []string{addr}
	cfg.Server = addr
	c, err := newClient(cfg)
	if err != nil {
		return ProtocolCheck{}, err
	}
	c.dial = func(ctx context.Context, target string) (net.Conn, error) {
		conn, dialErr := dial(ctx, "tcp", target)
		if dialErr != nil {
			return nil, dialErr
		}
		return c.secure(ctx, conn, target)
	}

	res := ProtocolCheck{Hostname: cfg.Hostname}
	switch {
	case len(cfg.TLS.PSK) > 0:
		res.Encryption = "PSK"
	case c.tlsConf != nil:
		res.Encryption = "TLS"
	}

	_, _, err = c.activeChecks(ctx)
	var hs *handshakeError
	switch {
	case err == nil:
		res.Answered, res.Host = true, HostKnown
	case errors.As(err, &hs):
		res.HandshakeErr = err
	case errors.Is(err, errHostUnknown):
		res.Answered, res.Host, res.HostInfo = true, HostUnknown, err.Error()
	case errors.Is(err, errServerRefused):
		res.Answered, res.Host, res.HostInfo = true, HostRefused, err.Error()
	default:
		res.ProtocolErr = fmt.Errorf("no Zabbix reply: %w", err)
	}
	return res, nil
}
