//go:build !linux && !windows

package hostnet

func platformForwarding() (v4, v6 string) { return "", "" }
