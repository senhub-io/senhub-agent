//go:build !linux

package configuration

// alignLicenseOwner is Linux-only: on Windows the folder ACL governs the
// sidecar, and darwin is a development target.
func alignLicenseOwner(_, _ string) error { return nil }
