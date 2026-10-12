//go:build unix

package agentstate

import "syscall"

// processCPUSeconds is the user plus system CPU time the process has
// really consumed, from getrusage. ok=false when the call fails.
func processCPUSeconds() (float64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	return timevalSeconds(ru.Utime) + timevalSeconds(ru.Stime), true
}

func timevalSeconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}
