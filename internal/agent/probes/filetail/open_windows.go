//go:build windows

package filetail

import (
	"os"

	"golang.org/x/sys/windows"
)

// openShared opens a file for reading without denying other processes the
// right to rename or delete it. The Go runtime's own open omits
// FILE_SHARE_DELETE, so a handle held by the probe would make the
// application that writes the log fail to rotate it.
func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
