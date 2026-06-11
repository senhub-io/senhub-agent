//go:build windows

package app

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isElevated reports whether the process token is elevated (UAC
// administrator). It queries TokenElevation directly instead of
// windows.Token.IsElevated so a query failure is distinguishable from
// a genuine "not elevated"; only on failure does it fall back to the
// historical probe of opening \\.\PHYSICALDRIVE0, which succeeds only
// for administrators but breaks on hosts without a PHYSICALDRIVE0
// (some VMs / storage drivers).
func isElevated() bool {
	var elevation uint32
	var outLen uint32
	err := windows.GetTokenInformation(
		windows.GetCurrentProcessToken(),
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&outLen,
	)
	if err != nil {
		f, openErr := os.Open(`\\.\PHYSICALDRIVE0`)
		if openErr != nil {
			return false
		}
		_ = f.Close()
		return true
	}
	return outLen == uint32(unsafe.Sizeof(elevation)) && elevation != 0
}
