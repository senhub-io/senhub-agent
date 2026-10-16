//go:build !linux

package cpu

// RunAdjtimexChild has nothing to do off Linux: no system call filter there
// can kill the agent on a clock read.
func RunAdjtimexChild() {}
