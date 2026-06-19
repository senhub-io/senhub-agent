//go:build !linux

package hostiface

// sysOperState has no portable non-Linux source for the carrier state; the
// caller falls back to the administrative IFF_UP flag.
func sysOperState(string) string { return "" }
