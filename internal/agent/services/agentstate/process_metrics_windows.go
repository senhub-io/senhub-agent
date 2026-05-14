//go:build windows

package agentstate

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	psapi                     = syscall.NewLazyDLL("psapi.dll")
	procGetCurrentProcess     = kernel32.NewProc("GetCurrentProcess")
	procGetProcessHandleCount = kernel32.NewProc("GetProcessHandleCount")
	procGetProcessMemoryInfo  = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS from psapi.h.
// We only care about WorkingSetSize (the analog of Linux VmRSS), but
// the syscall requires the full struct.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// getResidentMemory returns WorkingSetSize via GetProcessMemoryInfo.
// Equivalent to Task Manager's "Memory" column for the process.
// Returns 0 on syscall failure (caller treats 0 as unknown).
func getResidentMemory() uint64 {
	handle, _, _ := procGetCurrentProcess.Call()

	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))

	ret, _, _ := procGetProcessMemoryInfo.Call(
		handle,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.cb),
	)
	if ret == 0 {
		return 0
	}
	return uint64(counters.WorkingSetSize)
}

// getOpenFDs returns the process's open handle count via
// GetProcessHandleCount. On Windows, "handles" cover files, sockets,
// registry keys, events, threads, etc. — broader than Linux's FDs,
// but the leak-detection use case is identical: monotonic growth =
// problem.
func getOpenFDs() int {
	handle, _, _ := procGetCurrentProcess.Call()

	var count uint32
	ret, _, _ := procGetProcessHandleCount.Call(
		handle,
		uintptr(unsafe.Pointer(&count)),
	)
	if ret == 0 {
		return 0
	}
	return int(count)
}
