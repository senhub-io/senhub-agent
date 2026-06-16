//go:build !linux

package ipmi

import (
	"fmt"
	"runtime"
)

// runIpmitool is the non-Linux stub. IPMI via the in-kernel driver is
// only available on Linux; the probe still compiles on all platforms so
// a single binary can be distributed to mixed-OS fleets, but it will
// always emit senhub.ipmi.up=0 on non-Linux hosts with a clear log
// message explaining why.
func runIpmitool(_ ipmiConfig) (string, error) {
	return "", fmt.Errorf("ipmi probe is not supported on %s (requires Linux with OpenIPMI driver)", runtime.GOOS)
}
