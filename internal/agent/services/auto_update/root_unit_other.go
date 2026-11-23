//go:build !linux

package auto_update

// RegisterPackageInstaller does nothing off Linux: only deb and rpm installs
// have a root update unit.
func RegisterPackageInstaller() {}
