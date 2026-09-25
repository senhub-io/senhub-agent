//go:build !linux

package app

import "fmt"

// wireSystemdUnit is Linux/systemd-only. The systemd-creds backend and its unit
// wiring do not exist on Windows (DPAPI) or darwin (dev/test).
func wireSystemdUnit(_ string) error {
	return fmt.Errorf("wire-unit applies only to the Linux systemd-creds backend")
}

// followCredentialStore has nothing to follow outside systemd.
func followCredentialStore(_ string) {}

// removeCredentialsDropIn has nothing to remove outside systemd.
func removeCredentialsDropIn() error { return nil }
