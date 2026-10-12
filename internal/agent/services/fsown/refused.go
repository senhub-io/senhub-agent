package fsown

import (
	"errors"
	"io/fs"
)

// Refused reports a chmod or chown the filesystem does not do. An SMB or
// CIFS share (Azure Files) sets modes and owners from its mount options
// and answers "operation not permitted"; failing the write there would
// lose the file over a mode the share would never have kept anyway.
func Refused(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errors.ErrUnsupported)
}
