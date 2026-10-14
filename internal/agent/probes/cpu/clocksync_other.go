//go:build !linux && !windows

package cpu

func newClockSync() func() (clockSync, error) { return nil }
