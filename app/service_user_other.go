//go:build !linux

package app

// The dedicated service user is a Linux concept (hardened systemd
// unit); Windows and macOS service installs are unchanged.

func ensureServiceUser(_ string) error { return nil }

func chownServiceTree(_, _ string) error { return nil }
