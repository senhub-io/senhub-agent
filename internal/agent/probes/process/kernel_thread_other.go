//go:build !linux

package process

// isKernelThread is Linux-only; other OSes have no kernel-thread flag to read.
func isKernelThread(pid int32, procRoot string) bool {
	return false
}
