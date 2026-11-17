//go:build !linux

package netconn

// DumpTCP is not supported on this platform.
func DumpTCP() ([]Conn, error) {
	return nil, ErrUnsupported
}
