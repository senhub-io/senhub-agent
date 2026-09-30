//go:build windows

package hostnet

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetIpForwardTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIpForwardTable")

// platformRoutes reads the IPv4 routing table through the IP helper API,
// the table `route print` shows. Without it a Windows host carried no
// route, so nothing linked it to its gateway in the topology (#966).
func platformRoutes() ([]hostRoute, error) {
	var size uint32
	r, _, _ := procGetIpForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) && r != 0 {
		return nil, fmt.Errorf("GetIpForwardTable size: error %d", r)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	r, _, _ = procGetIpForwardTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
	if r != 0 {
		return nil, fmt.Errorf("GetIpForwardTable: error %d", r)
	}
	return parseForwardTable(buf[:size], func(idx int) string {
		if ifc, err := net.InterfaceByIndex(idx); err == nil {
			return ifc.Name
		}
		return ""
	}), nil
}
