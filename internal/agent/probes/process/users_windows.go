//go:build windows

package process

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no login accounting file: the sessions live in the
// terminal services subsystem, which is what serves the local console
// and any remote desktop alike. Counting the connected ones is the
// closest thing to what `who` lists on Unix, and it is what the native
// Zabbix agent reports for system.users.num on this platform.

var (
	wtsapi32                = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSEnumerateSession = wtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSFreeMemory       = wtsapi32.NewProc("WTSFreeMemory")
)

// wtsSessionInfo mirrors WTS_SESSION_INFOW.
type wtsSessionInfo struct {
	SessionID      uint32
	WinStationName *uint16
	State          uint32
}

// WTS_CONNECTSTATE_CLASS. A session counts when someone is logged on to
// it: either working in it (WTSActive) or logged on with the client
// detached (WTSDisconnected), which is what an administrator sees as
// "Disc" in `query user` and which still holds that user's processes.
// WTSConnected is deliberately not counted: it is a client attached to
// a station before anyone has logged on. The remaining states are
// listening stations and sessions on their way in or out.
const (
	wtsActive       = 0
	wtsDisconnected = 4
)

func loggedInSessions() (float64, error) {
	var sessions *wtsSessionInfo
	var count uint32
	// WTS_CURRENT_SERVER_HANDLE is NULL: the machine this runs on.
	ret, _, err := procWTSEnumerateSession.Call(
		0, 0, 1,
		uintptr(unsafe.Pointer(&sessions)),
		uintptr(unsafe.Pointer(&count)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("WTSEnumerateSessionsW: %w", err)
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(sessions))) //nolint:errcheck // freeing returns nothing to check

	list := unsafe.Slice(sessions, count)
	open := 0
	for _, s := range list {
		if s.State == wtsActive || s.State == wtsDisconnected {
			open++
		}
	}
	return float64(open), nil
}
