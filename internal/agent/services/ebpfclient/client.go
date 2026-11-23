// Package ebpfclient is the agent's side of the senhub-ebpf helper: an
// optional, unprivileged reader of the helper's unix socket. When the socket is
// absent every call fails fast and the agent behaves as if the helper did not
// exist.
package ebpfclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"senhub-agent.go/internal/ebpfproto"
)

const defaultTimeout = 10 * time.Second

// Client talks to one helper socket. It opens a connection per call, so a
// helper restart needs no reconnection logic.
type Client struct {
	path    string
	timeout time.Duration
}

// New returns a client for the socket at path; an empty path selects the
// default location.
func New(path string) *Client {
	if path == "" {
		path = ebpfproto.DefaultSocket
	}
	return &Client{path: path, timeout: defaultTimeout}
}

// Present reports whether the socket file exists, without connecting.
func (c *Client) Present() bool {
	_, err := os.Stat(c.path)
	return err == nil
}

func (c *Client) call(op string) (ebpfproto.Response, error) {
	d := net.Dialer{Timeout: c.timeout}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return ebpfproto.Response{}, fmt.Errorf("connecting to %s: %w", c.path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	if err := ebpfproto.Write(conn, ebpfproto.Request{V: ebpfproto.Version, Op: op}); err != nil {
		return ebpfproto.Response{}, err
	}
	resp, err := ebpfproto.ReadResponse(ebpfproto.NewReader(conn))
	if err != nil {
		return resp, fmt.Errorf("reading %s answer: %w", op, err)
	}
	return resp, nil
}

// Connections returns the helper's current view of TCP connections. It fails
// when the helper is absent, not running, or speaks another protocol version.
func (c *Client) Connections() ([]ebpfproto.Conn, error) {
	if !c.Present() {
		return nil, os.ErrNotExist
	}
	resp, err := c.call(ebpfproto.OpSnapshot)
	if err != nil {
		return nil, err
	}
	return resp.Conns, nil
}

// Hello asks the helper to describe itself.
func (c *Client) Hello() (ebpfproto.Response, error) {
	if !c.Present() {
		return ebpfproto.Response{}, os.ErrNotExist
	}
	return c.call(ebpfproto.OpHello)
}

// IsAbsent reports whether err means the helper is simply not there, as
// opposed to there and failing.
func IsAbsent(err error) bool { return errors.Is(err, os.ErrNotExist) }
