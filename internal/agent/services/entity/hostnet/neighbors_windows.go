//go:build windows

package hostnet

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetIpNetTable2 = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIpNetTable2")
	procFreeMibTable   = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("FreeMibTable")
)

func platformNeighbors() ([]hostNeighbor, error) {
	var table *byte
	r, _, _ := procGetIpNetTable2.Call(uintptr(windows.AF_UNSPEC), uintptr(unsafe.Pointer(&table)))
	if r != 0 {
		return nil, fmt.Errorf("GetIpNetTable2: error %d", r)
	}
	if table == nil {
		return nil, nil
	}
	defer func() {
		_, _, _ = procFreeMibTable.Call(uintptr(unsafe.Pointer(table)))
	}()

	n := *(*uint32)(unsafe.Pointer(table))
	size := ipnetTableHeaderSize + int(n)*ipnetRowSize
	buf := unsafe.Slice((*byte)(unsafe.Pointer(table)), size)
	return dedupeAndSortNeighbors(parseIpNetTable2(buf, func(idx int) string {
		if ifc, err := net.InterfaceByIndex(idx); err == nil {
			return ifc.Name
		}
		return ""
	})), nil
}
