package oracle

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"

	go_ora "github.com/sijms/go-ora/v2"
)

// go-ora (checked up to v2.9.0 and master of 2026-09) never sets the
// "long password" bit (0x80) in the logon-types byte of the client TTC
// capabilities. Oracle 23ai then treats the client as a pre-12.2 one and
// refuses any password longer than 30 characters with ORA-01017, while
// SQL*Plus, which sets the bit, logs in with the same credentials.
// Releases before 23ai cap the password at 30 bytes anyway, so setting the
// bit is harmless there.
//
// The capabilities are a hard-coded array inside the driver, so the bit is
// raised on the wire: the data type negotiation packet carries the array,
// and the dialer below flips that one byte as it leaves.
var (
	// ttcCapsPrefix is the start of the driver's compile-time capabilities
	// array; ttcLogonTypesIndex is the logon-types byte inside it.
	ttcCapsPrefix      = []byte{6, 1, 0, 0, 106, 1, 1, 11, 1, 1, 1, 1, 1, 1, 0, 41, 144, 3, 7, 3}
	ttcLogonTypesIndex = 4
)

const (
	ttcLongPasswordBit = 0x80
	// The negotiation is among the first packets of a session; past this
	// many writes the array is not coming (TLS, or a driver that changed it).
	ttcPatchWindow = 8
)

// longPasswordDialer wraps every connection it opens so that the driver
// announces long password support.
type longPasswordDialer struct {
	net.Dialer
}

func (d *longPasswordDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.Dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &longPasswordConn{Conn: conn}, nil
}

// longPasswordConn is used by one session, from one goroutine at a time:
// the driver writes its packets sequentially.
type longPasswordConn struct {
	net.Conn

	writes int
}

// Write patches the capabilities array in the first outgoing packet that
// holds it. A packet that does not carry the exact array, including one
// from a future driver release that changed it, is sent untouched.
func (c *longPasswordConn) Write(p []byte) (int, error) {
	if c.writes < ttcPatchWindow {
		c.writes++
		if i := bytes.Index(p, ttcCapsPrefix); i >= 0 {
			patched := make([]byte, len(p))
			copy(patched, p)
			patched[i+ttcLogonTypesIndex] |= ttcLongPasswordBit
			c.writes = ttcPatchWindow
			p = patched
		}
	}
	return c.Conn.Write(p)
}

// openDB opens a database/sql handle on a go-ora DSN with the long
// password repair in place. Like sql.Open it does not dial.
func openDB(dsn string) (*sql.DB, error) {
	connector, ok := go_ora.NewConnector(dsn).(*go_ora.OracleConnector)
	if !ok {
		return nil, errors.New("go-ora returned an unexpected connector type")
	}
	connector.Dialer(&longPasswordDialer{})
	return sql.OpenDB(connector), nil
}
