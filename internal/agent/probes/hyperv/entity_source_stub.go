//go:build !windows

package hyperv

// kvpGuestMachineID is a no-op stub on non-Windows platforms. The KVP data
// exchange channel is a Hyper-V WMI facility; the probe itself is already
// gated to Windows only at OnStart. This stub lets the entity source compile
// everywhere so the pure logic in entity_source.go can be tested
// cross-platform without a Windows build constraint.
func kvpGuestMachineID(_ string) string { return "" }
