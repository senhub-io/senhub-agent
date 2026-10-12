//go:build !linux

package cpu

func newClockSync() func() (clockSync, error) { return nil }
