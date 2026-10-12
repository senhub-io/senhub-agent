//go:build windows

package agentstate

import "syscall"

// processCPUSeconds is the kernel plus user CPU time the process has
// really consumed, from GetProcessTimes. ok=false when the call fails.
func processCPUSeconds() (float64, bool) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, false
	}
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return filetimeSeconds(kernel) + filetimeSeconds(user), true
}

// filetimeSeconds reads a FILETIME duration, counted in 100 ns units.
func filetimeSeconds(ft syscall.Filetime) float64 {
	return float64(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) / 1e7
}
